package proxy

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dhawalhost/automell/types"
)

// tryOptimize handles trivial requests locally. Returns true if handled.
func tryOptimize(w http.ResponseWriter, req *types.AnthropicRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}

	lastContent := lastTextContent(req)

	if isNetworkProbe(lastContent, req.MaxTokens) {
		respondJSON(w, quickResponse("OK", req.Model))
		return true
	}
	if isTitleGeneration(lastContent) {
		respondJSON(w, quickResponse("New Conversation", req.Model))
		return true
	}
	if isSuggestionMode(lastContent) {
		respondJSON(w, quickResponse("[]", req.Model))
		return true
	}
	if isFilepathExtraction(lastContent) {
		respondJSON(w, quickResponse("[]", req.Model))
		return true
	}
	if isPrefixDetection(lastContent) {
		respondJSON(w, quickResponse("", req.Model))
		return true
	}
	return false
}

func lastTextContent(req *types.AnthropicRequest) string {
	last := req.Messages[len(req.Messages)-1]
	switch v := last.Content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if t, ok := block["text"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
		}
		return sb.String()
	}
	return ""
}

func quickResponse(text, model string) interface{} {
	stopReason := "end_turn"
	return types.AResponse{
		ID:    "msg_" + generateID(),
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Content: []types.AContentBlock{
			{Type: "text", Text: text},
		},
		StopReason: &stopReason,
		Usage:      types.AUsage{InputTokens: 10, OutputTokens: len(strings.Fields(text)) + 1},
	}
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// isNetworkProbe detects network probe requests
func isNetworkProbe(content string, maxTokens int) bool {
	if maxTokens > 5 {
		return false
	}
	probes := []string{"ping", "hello", "test", "status", "health"}
	lower := strings.ToLower(strings.TrimSpace(content))
	for _, p := range probes {
		if lower == p {
			return true
		}
	}
	return false
}

// isTitleGeneration detects title-generation requests
func isTitleGeneration(content string) bool {
	lower := strings.ToLower(content)
	return (strings.Contains(lower, "generate") || strings.Contains(lower, "create") || strings.Contains(lower, "write")) &&
		strings.Contains(lower, "title") &&
		len(content) < 300
}

// isSuggestionMode detects suggestion-mode requests
func isSuggestionMode(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "suggest") &&
		(strings.Contains(lower, "follow-up") || strings.Contains(lower, "followup") || strings.Contains(lower, "question"))
}

// isFilepathExtraction detects filepath extraction requests
func isFilepathExtraction(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "extract") &&
		(strings.Contains(lower, "file") || strings.Contains(lower, "path")) &&
		strings.Contains(lower, "json")
}

// isPrefixDetection detects prefix/completion requests
func isPrefixDetection(content string) bool {
	return strings.HasSuffix(strings.TrimSpace(content), "...") ||
		strings.Contains(strings.ToLower(content), "complete the following")
}
