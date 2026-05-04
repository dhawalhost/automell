package proxy

import "testing"

func TestExtractTextFromContent_NestedBlocks(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"type": "text", "text": "hello "},
		map[string]interface{}{"type": "tool_result", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "world"},
		}},
		map[string]interface{}{"type": "thinking", "thinking": "!"},
	}

	got := extractTextFromContent(input)
	want := "hello world!"

	if got != want {
		t.Fatalf("extractTextFromContent mismatch: got %q want %q", got, want)
	}
}
