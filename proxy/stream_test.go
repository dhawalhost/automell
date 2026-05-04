package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dhawalhost/automell/types"
)

func TestStreamTranslator_InterleavedToolDeltasKeepBlockIndex(t *testing.T) {
	makeChunk := func(delta types.OAIDelta, finish *string) string {
		chunk := types.OAIStreamChunk{
			ID:      "c1",
			Object:  "chat.completion.chunk",
			Created: 1,
			Model:   "test-model",
			Choices: []types.OAIChoice{{
				Index:        0,
				Delta:        delta,
				FinishReason: finish,
			}},
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			t.Fatalf("failed to marshal chunk: %v", err)
		}
		return "data: " + string(b) + "\n\n"
	}

	finish := "tool_calls"
	stream := strings.Builder{}
	stream.WriteString(makeChunk(types.OAIDelta{ToolCalls: []types.OAIToolCallDelta{{
		Index: 0,
		ID:    "tc_0",
		Type:  "function",
		Function: types.OAIFuncDelta{
			Name:      "tool0",
			Arguments: "{\"a\":",
		},
	}}}, nil))
	stream.WriteString(makeChunk(types.OAIDelta{ToolCalls: []types.OAIToolCallDelta{{
		Index: 1,
		ID:    "tc_1",
		Type:  "function",
		Function: types.OAIFuncDelta{
			Name:      "tool1",
			Arguments: "{\"b\":",
		},
	}}}, nil))
	stream.WriteString(makeChunk(types.OAIDelta{ToolCalls: []types.OAIToolCallDelta{{
		Index: 0,
		Function: types.OAIFuncDelta{
			Arguments: "1}",
		},
	}}}, nil))
	stream.WriteString(makeChunk(types.OAIDelta{ToolCalls: []types.OAIToolCallDelta{{
		Index: 1,
		Function: types.OAIFuncDelta{
			Arguments: "2}",
		},
	}}}, &finish))
	stream.WriteString("data: [DONE]\n\n")

	out := strings.Builder{}
	translator := NewStreamTranslator(&out, false)
	if err := translator.Run(strings.NewReader(stream.String()), "claude-sonnet-4-5"); err != nil {
		t.Fatalf("translator run failed: %v", err)
	}

	var inputJSONDeltaIndexes []int
	var toolStopIndexes []int

	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			continue
		}

		typ, _ := ev["type"].(string)
		if typ == "content_block_delta" {
			delta, _ := ev["delta"].(map[string]interface{})
			deltaType, _ := delta["type"].(string)
			if deltaType == "input_json_delta" {
				idx, _ := ev["index"].(float64)
				inputJSONDeltaIndexes = append(inputJSONDeltaIndexes, int(idx))
			}
		}

		if typ == "content_block_stop" {
			idx, _ := ev["index"].(float64)
			toolStopIndexes = append(toolStopIndexes, int(idx))
		}
	}

	expected := []int{0, 1, 0, 1}
	if len(inputJSONDeltaIndexes) != len(expected) {
		t.Fatalf("unexpected input_json_delta count: got %v want %v", inputJSONDeltaIndexes, expected)
	}
	for i := range expected {
		if inputJSONDeltaIndexes[i] != expected[i] {
			t.Fatalf("unexpected input_json_delta indexes: got %v want %v", inputJSONDeltaIndexes, expected)
		}
	}

	// At least two stop events for the two tool blocks should be present at indexes 0 and 1.
	found0 := false
	found1 := false
	for _, idx := range toolStopIndexes {
		if idx == 0 {
			found0 = true
		}
		if idx == 1 {
			found1 = true
		}
	}
	if !found0 || !found1 {
		t.Fatalf("missing expected tool block stop indexes in %v", toolStopIndexes)
	}
}
