package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dhawalhost/automell/types"
)

// streamTranslator handles the translation of OpenAI SSE streams to Anthropic SSE format
type streamTranslator struct {
	writer           io.Writer
	logSSE           bool // when true, log each raw SSE line
	blockIndex       int
	currentBlock     int
	inContentBlock   bool
	currentBlockType string
	// accumulated tool call args keyed by delta index
	toolArgs       map[int]string
	toolMeta       map[int]*types.OAIToolCallDelta
	toolBlockIndex map[int]int
	toolBlockOpen  map[int]bool
	tagBuf         string
	// final usage stats from provider (populated when the last chunk carries usage)
	finalUsage *types.OAIUsage
	mu         sync.Mutex
	finalized  bool
}

// NewStreamTranslator creates a new stream translator
func NewStreamTranslator(writer io.Writer, logSSE bool) *streamTranslator {
	return &streamTranslator{
		writer:         writer,
		logSSE:         logSSE,
		toolArgs:       make(map[int]string),
		toolMeta:       make(map[int]*types.OAIToolCallDelta),
		toolBlockIndex: make(map[int]int),
		toolBlockOpen:  make(map[int]bool),
	}
}

// Run processes the OpenAI SSE stream and writes Anthropic SSE events
func (st *streamTranslator) Run(reader io.Reader, model string) error {
	scanner := bufio.NewScanner(reader)
	// Default 64 KB is too small for SSE lines carrying large tool-call inputs.
	// Raise to 8 MB to reduce scanner token-limit failures on large JSON arguments.
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	stopReason := "end_turn"
	st.sendEvent(types.MessageStartEvent{
		Type: "message_start",
		Message: types.AResponse{
			ID:    "msg_" + generateID(),
			Type:  "message",
			Role:  "assistant",
			Model: model,
			Usage: types.AUsage{},
		},
	})

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		if st.logSSE {
			fmt.Printf("[DEBUG SSE] %s\n", data)
		}

		var chunk types.OAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Capture usage from any chunk that includes it (providers differ on timing)
		if chunk.Usage != nil {
			st.finalUsage = chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Reasoning / thinking content
		if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
			if err := st.handleThinkingDelta(*delta.ReasoningContent); err != nil {
				return err
			}
		}

		// Tool call deltas
		for _, tc := range delta.ToolCalls {
			if err := st.handleToolCallDelta(tc); err != nil {
				return err
			}
		}

		// Text content
		if delta.Content != nil {
			content := st.processTags(*delta.Content)
			if content != "" {
				if err := st.handleTextDelta(content); err != nil {
					return err
				}
			}
		}

		// Finish reason
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			stopReason = mapFinishReason(*choice.FinishReason)
		}
	}

	// Close any open text/thinking content block
	if st.inContentBlock {
		st.sendEvent(types.ContentBlockStopEvent{
			Type:  "content_block_stop",
			Index: st.currentBlock,
		})
		st.inContentBlock = false
	}
	st.closeOpenToolBlocks()

	// Build usage from provider data when available
	usage := types.AUsage{}
	if st.finalUsage != nil {
		usage.InputTokens = st.finalUsage.PromptTokens
		usage.OutputTokens = st.finalUsage.CompletionTokens
	}

	// Emit message_delta with stop reason and real usage
	st.sendEvent(types.MessageDeltaEvent{
		Type: "message_delta",
		Delta: types.MessageDelta{
			StopReason: &stopReason,
		},
		Usage: usage,
	})
	st.sendEvent(types.MessageStopEvent{Type: "message_stop"})

	return scanner.Err()
}

// processTags buffers partial <thinking> tags
func (st *streamTranslator) processTags(content string) string {
	st.tagBuf += content
	result := ""
	for {
		if strings.Contains(st.tagBuf, "<thinking>") || strings.Contains(st.tagBuf, "</thinking>") {
			break
		}
		if strings.HasPrefix(st.tagBuf, "<") {
			// Might be start of a tag — wait for more
			potentialTag := "<thinking>"
			matched := false
			for i := 1; i <= len(potentialTag) && i <= len(st.tagBuf); i++ {
				if st.tagBuf[:i] == potentialTag[:i] {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		result += st.tagBuf
		st.tagBuf = ""
		break
	}
	return result
}

// handleTextDelta emits text content deltas
func (st *streamTranslator) handleTextDelta(content string) error {
	st.closeOpenToolBlocks()

	// Switch block type if needed
	if st.inContentBlock && st.currentBlockType != "text" {
		st.sendEvent(types.ContentBlockStopEvent{Type: "content_block_stop", Index: st.currentBlock})
		st.inContentBlock = false
	}
	if !st.inContentBlock {
		idx := st.blockIndex
		st.currentBlock = idx
		st.blockIndex++
		st.sendEvent(types.ContentBlockStartEvent{
			Type:  "content_block_start",
			Index: idx,
			ContentBlock: types.ContentBlockStart{
				Type: "text",
				Text: "",
			},
		})
		st.inContentBlock = true
		st.currentBlockType = "text"
	}
	st.sendEvent(types.ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: st.currentBlock,
		Delta: types.ContentDelta{Type: "text_delta", Text: content},
	})
	return nil
}

// handleThinkingDelta emits thinking block deltas
func (st *streamTranslator) handleThinkingDelta(content string) error {
	st.closeOpenToolBlocks()

	if st.inContentBlock && st.currentBlockType != "thinking" {
		st.sendEvent(types.ContentBlockStopEvent{Type: "content_block_stop", Index: st.currentBlock})
		st.inContentBlock = false
	}
	if !st.inContentBlock {
		idx := st.blockIndex
		st.currentBlock = idx
		st.blockIndex++
		st.sendEvent(types.ContentBlockStartEvent{
			Type:         "content_block_start",
			Index:        idx,
			ContentBlock: types.ContentBlockStart{Type: "thinking"},
		})
		st.inContentBlock = true
		st.currentBlockType = "thinking"
	}
	st.sendEvent(types.ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: st.currentBlock,
		Delta: types.ContentDelta{Type: "thinking_delta", Thinking: content},
	})
	return nil
}

// handleToolCallDelta accumulates and emits tool-call deltas
func (st *streamTranslator) handleToolCallDelta(tc types.OAIToolCallDelta) error {
	idx := tc.Index

	if _, exists := st.toolMeta[idx]; !exists {
		// New tool call — close open text/thinking block and start a new tool_use block.
		if st.inContentBlock {
			st.sendEvent(types.ContentBlockStopEvent{Type: "content_block_stop", Index: st.currentBlock})
			st.inContentBlock = false
		}

		anthropicBlockIndex := st.blockIndex
		st.blockIndex++
		st.toolMeta[idx] = &tc
		st.toolArgs[idx] = ""
		st.toolBlockIndex[idx] = anthropicBlockIndex
		st.toolBlockOpen[idx] = true

		st.sendEvent(types.ContentBlockStartEvent{
			Type:  "content_block_start",
			Index: anthropicBlockIndex,
			ContentBlock: types.ContentBlockStart{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: map[string]interface{}{},
			},
		})
	}

	// Accumulate args and emit partial JSON delta
	if tc.Function.Arguments != "" {
		st.toolArgs[idx] += tc.Function.Arguments
		anthropicBlockIndex, ok := st.toolBlockIndex[idx]
		if !ok {
			return nil
		}
		st.sendEvent(types.ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: anthropicBlockIndex,
			Delta: types.ContentDelta{
				Type:        "input_json_delta",
				PartialJSON: tc.Function.Arguments,
			},
		})
	}
	return nil
}

func (st *streamTranslator) closeOpenToolBlocks() {
	if len(st.toolBlockOpen) == 0 {
		return
	}

	type toolBlock struct {
		idx int
		pos int
	}

	blocks := make([]toolBlock, 0, len(st.toolBlockOpen))
	for idx, open := range st.toolBlockOpen {
		if !open {
			continue
		}
		pos, ok := st.toolBlockIndex[idx]
		if !ok {
			continue
		}
		blocks = append(blocks, toolBlock{idx: idx, pos: pos})
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].pos < blocks[j].pos
	})

	for _, b := range blocks {
		st.sendEvent(types.ContentBlockStopEvent{Type: "content_block_stop", Index: b.pos})
		st.toolBlockOpen[b.idx] = false
	}
}

// sendEvent serialises and writes a single SSE event
func (st *streamTranslator) sendEvent(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	// Determine event name from the "type" field
	var wrapper map[string]interface{}
	json.Unmarshal(b, &wrapper)
	eventType, _ := wrapper["type"].(string)
	if eventType == "" {
		eventType = "event"
	}
	fmt.Fprintf(st.writer, "event: %s\ndata: %s\n\n", eventType, b)
	// Flush if possible
	if f, ok := st.writer.(interface{ Flush() }); ok {
		f.Flush()
	}
}

// generateID generates a simple nanosecond-based ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// finalize cleans up the stream translator
func (st *streamTranslator) finalize() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.finalized = true
}
