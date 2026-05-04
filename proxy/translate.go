package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dhawalhost/automell/config"
	"github.com/dhawalhost/automell/types"
)

// translateRequest converts an Anthropic request to an OpenAI request
func translateRequest(req *types.AnthropicRequest, providerModel string) (*types.OAIRequest, error) {
	oaiReq := &types.OAIRequest{
		Model:       providerModel,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}
	if req.Stream {
		oaiReq.StreamOptions = &types.StreamOptions{IncludeUsage: true}
	}

	// Thinking toggle: check config for the original Claude model name.
	// req.Model is the original claude-* name before provider resolution.
	// Models ending in "-no-thinking" (e.g. "claude-sonnet-4-5-no-thinking") always disable thinking.
	cfg := config.Get()
	effectiveModel := req.Model
	noThinkingVariant := strings.HasSuffix(strings.ToLower(effectiveModel), "-no-thinking")
	thinkingEnabled := !noThinkingVariant && cfg.ThinkingEnabledForModel(effectiveModel)

	// If thinking is enabled and the client requested it, forward the budget hint
	// as a provider-level thinking parameter where supported.
	// (Most OAI-compat providers ignore unknown fields; we include it anyway.)
	if thinkingEnabled && req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
		oaiReq.Thinking = req.Thinking
	}

	// Strip thinking budget when disabled so providers don't accidentally enable it.
	// (Already nil when thinkingEnabled is false and client didn't send it.)

	// Stop sequences
	if len(req.StopSequences) > 0 {
		oaiReq.Stop = req.StopSequences
	}

	// System prompt
	systemText := extractSystemText(req.System)
	if systemText != "" {
		oaiReq.Messages = append(oaiReq.Messages, types.OAIMessage{
			Role:    "system",
			Content: systemText,
		})
	}

	// Convert messages, optionally stripping thinking blocks
	for _, msg := range req.Messages {
		converted, err := convertAMessage(msg, thinkingEnabled)
		if err != nil {
			return nil, err
		}
		oaiReq.Messages = append(oaiReq.Messages, converted...)
	}

	// Convert tools
	for _, tool := range req.Tools {
		oaiReq.Tools = append(oaiReq.Tools, types.OAITool{
			Type: "function",
			Function: types.OAIFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	return oaiReq, nil
}

// extractSystemText extracts the system prompt from Anthropic's system field
func extractSystemText(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if typ, ok := block["type"].(string); ok && typ == "text" {
					if text, ok := block["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// convertAMessage converts an AMessage to one or more OAIMessages
func convertAMessage(msg types.AMessage, thinkingEnabled bool) ([]types.OAIMessage, error) {
	// Content can be a string or []AContentBlock
	switch v := msg.Content.(type) {
	case string:
		return []types.OAIMessage{{Role: msg.Role, Content: v}}, nil
	case []interface{}:
		return convertContentArray(msg.Role, v, thinkingEnabled)
	default:
		// Try marshalling back as JSON and re-parsing as blocks
		b, err := json.Marshal(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("unsupported content type: %T", msg.Content)
		}
		var blocks []map[string]interface{}
		if err := json.Unmarshal(b, &blocks); err != nil {
			return nil, fmt.Errorf("unsupported content type: %T", msg.Content)
		}
		var ifaces []interface{}
		for _, b := range blocks {
			ifaces = append(ifaces, b)
		}
		return convertContentArray(msg.Role, ifaces, thinkingEnabled)
	}
}

func convertContentArray(role string, blocks []interface{}, thinkingEnabled bool) ([]types.OAIMessage, error) {
	var userParts []interface{}
	var toolResultMsgs []types.OAIMessage
	var assistantToolCalls []types.OAIToolCall

	for _, item := range blocks {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := block["type"].(string)

		// Skip thinking blocks when thinking is disabled — they confuse providers
		// that don't support extended thinking.
		if typ == "thinking" && !thinkingEnabled {
			continue
		}

		switch typ {
		case "tool_use":
			if role != "assistant" {
				userParts = append(userParts, block)
				continue
			}

			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			if name == "" {
				continue
			}

			inputRaw := "{}"
			if input, exists := block["input"]; exists && input != nil {
				if b, err := json.Marshal(input); err == nil {
					inputRaw = string(b)
				}
			}

			assistantToolCalls = append(assistantToolCalls, types.OAIToolCall{
				ID:   id,
				Type: "function",
				Function: types.OAIFuncCall{
					Name:      name,
					Arguments: inputRaw,
				},
			})
		case "tool_result":
			toolUseID, _ := block["tool_use_id"].(string)
			contentRaw := block["content"]
			toolContent, err := toolResultContent(contentRaw)
			if err != nil {
				return nil, err
			}
			toolResultMsgs = append(toolResultMsgs, types.OAIMessage{
				Role:       "tool",
				Content:    toolContent,
				ToolCallID: toolUseID,
			})
		default:
			userParts = append(userParts, block)
		}
	}

	var result []types.OAIMessage
	if len(userParts) > 0 {
		content, err := buildOAIContent(userParts)
		if err != nil {
			return nil, err
		}
		result = append(result, types.OAIMessage{Role: role, Content: content, ToolCalls: assistantToolCalls})
	} else if role == "assistant" && len(assistantToolCalls) > 0 {
		// For tool-only assistant turns, send empty content with tool_calls.
		result = append(result, types.OAIMessage{Role: role, Content: "", ToolCalls: assistantToolCalls})
	}
	result = append(result, toolResultMsgs...)
	return result, nil
}

func toolResultContent(raw interface{}) (interface{}, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []interface{}:
		// Array of content blocks - extract text
		var texts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if t, ok := block["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
		}
		return strings.Join(texts, "\n"), nil
	default:
		if raw == nil {
			return "", nil
		}
		b, _ := json.Marshal(raw)
		return string(b), nil
	}
}

func buildOAIContent(parts []interface{}) (interface{}, error) {
	if len(parts) == 1 {
		if block, ok := parts[0].(map[string]interface{}); ok {
			if block["type"] == "text" {
				if t, ok := block["text"].(string); ok {
					return t, nil
				}
			}
		}
	}
	var content []types.OAIContentPart
	for _, item := range parts {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := block["type"].(string)
		if typ == "text" {
			text, _ := block["text"].(string)
			content = append(content, types.OAIContentPart{Type: "text", Text: text})
		} else if typ == "thinking" {
			// Wrap thinking in special tag for providers
			thinking, _ := block["thinking"].(string)
			content = append(content, types.OAIContentPart{
				Type: "text",
				Text: fmt.Sprintf("<thinking>%s</thinking>", thinking),
			})
		}
	}
	return content, nil
}

// translateNonStreamResponse converts an OpenAI response to an Anthropic response
func translateNonStreamResponse(oaiResp *types.OAIResponse) (*types.AResponse, error) {
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := oaiResp.Choices[0]
	content, err := convertOAIMessageToAnthropic(choice.Message)
	if err != nil {
		return nil, err
	}

	stopReason := mapFinishReason(choice.FinishReason)
	return &types.AResponse{
		ID:         oaiResp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		StopReason: &stopReason,
		Model:      oaiResp.Model,
		Usage: types.AUsage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		},
	}, nil
}

// convertOAIMessageToAnthropic converts an OAI message to Anthropic content blocks
func convertOAIMessageToAnthropic(msg types.OAIMessage) ([]types.AContentBlock, error) {
	var blocks []types.AContentBlock

	// Tool calls
	for _, tc := range msg.ToolCalls {
		inputRaw := json.RawMessage(tc.Function.Arguments)
		if len(inputRaw) == 0 {
			inputRaw = json.RawMessage("{}")
		}
		block := types.AContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: inputRaw,
		}
		// Subagent: force run_in_background=false for Task tool calls
		if tc.Function.Name == "Task" {
			block.Input = forceSubagentForeground(inputRaw)
		}
		blocks = append(blocks, block)
	}

	// Text content
	switch v := msg.Content.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			if idx := strings.Index(v, "<thinking>"); idx >= 0 {
				endIdx := strings.Index(v, "</thinking>")
				if endIdx > idx {
					thinkingText := v[idx+len("<thinking>") : endIdx]
					blocks = append(blocks, types.AContentBlock{
						Type:     "thinking",
						Thinking: thinkingText,
					})
					after := strings.TrimSpace(v[endIdx+len("</thinking>"):])
					if after != "" {
						blocks = append(blocks, types.AContentBlock{Type: "text", Text: after})
					}
				} else {
					blocks = append(blocks, types.AContentBlock{Type: "text", Text: v})
				}
			} else {
				blocks = append(blocks, types.AContentBlock{Type: "text", Text: v})
			}
		}
	case []interface{}:
		for _, part := range v {
			if appendOAIContentPart(&blocks, part) {
				continue
			}
		}
	case map[string]interface{}:
		appendOAIContentPart(&blocks, v)
	}

	return blocks, nil
}

func appendOAIContentPart(blocks *[]types.AContentBlock, part interface{}) bool {
	m, ok := part.(map[string]interface{})
	if !ok {
		return false
	}

	typ, _ := m["type"].(string)
	switch typ {
	case "text", "output_text":
		if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
			*blocks = append(*blocks, types.AContentBlock{Type: "text", Text: text})
			return true
		}
	case "thinking", "reasoning":
		if thinking, ok := m["thinking"].(string); ok && strings.TrimSpace(thinking) != "" {
			*blocks = append(*blocks, types.AContentBlock{Type: "thinking", Thinking: thinking})
			return true
		}
		if reasoning, ok := m["reasoning"].(string); ok && strings.TrimSpace(reasoning) != "" {
			*blocks = append(*blocks, types.AContentBlock{Type: "thinking", Thinking: reasoning})
			return true
		}
	}

	return false
}

// mapFinishReason maps OpenAI finish reasons to Anthropic stop reasons
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}
