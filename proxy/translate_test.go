package proxy

import (
	"testing"

	"github.com/dhawalhost/automell/types"
)

func TestConvertOAIMessageToAnthropic_ArrayContent(t *testing.T) {
	msg := types.OAIMessage{
		Content: []interface{}{
			map[string]interface{}{"type": "output_text", "text": "hello"},
			map[string]interface{}{"type": "reasoning", "reasoning": "because"},
		},
	}

	blocks, err := convertOAIMessageToAnthropic(msg)
	if err != nil {
		t.Fatalf("convertOAIMessageToAnthropic returned error: %v", err)
	}

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	if blocks[0].Type != "text" || blocks[0].Text != "hello" {
		t.Fatalf("unexpected first block: %#v", blocks[0])
	}

	if blocks[1].Type != "thinking" || blocks[1].Thinking != "because" {
		t.Fatalf("unexpected second block: %#v", blocks[1])
	}
}
