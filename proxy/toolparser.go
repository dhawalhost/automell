package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dhawalhost/automell/types"
)

// TryParseToolCalls attempts to parse tool calls from raw text
// Returns tool calls if found, nil otherwise
func TryParseToolCalls(content string, toolNames []string) ([]map[string]interface{}, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	// Pattern 1: Whole response is a JSON object with tool call structure
	if strings.HasPrefix(content, "{") && strings.HasSuffix(content, "}") {
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(content), &result); err == nil {
			if toolCalls, ok := extractToolCallsFromJSON(result, toolNames); ok {
				return toolCalls, nil
			}
		}
	}

	// Pattern 2: JSON inside a code block
	if strings.Contains(content, "```json") || strings.Contains(content, "```") {
		if toolCalls, err := parseToolCallsFromCodeBlock(content, toolNames); err == nil && toolCalls != nil {
			return toolCalls, nil
		}
	}

	// Pattern 3: XML tags <invoke>...</invoke>
	if strings.Contains(content, "<invoke>") && strings.Contains(content, "</invoke>") {
		if toolCalls, err := parseToolCallsFromXML(content, toolNames); err == nil && toolCalls != nil {
			return toolCalls, nil
		}
	}

	// Pattern 4: Function call syntax functionName({...})
	if toolCalls, err := parseToolCallsFromFunctionSyntax(content, toolNames); err == nil && toolCalls != nil {
		return toolCalls, nil
	}

	return nil, nil
}

// extractToolCallsFromJSON extracts tool calls from a JSON object
func extractToolCallsFromJSON(result map[string]interface{}, toolNames []string) ([]map[string]interface{}, bool) {
	// Check for common tool call keys
	keys := []string{"name", "function", "tool", "tool_name"}
	var name string
	for _, key := range keys {
		if val, ok := result[key].(string); ok {
			name = val
			break
		}
	}

	if name == "" {
		return nil, false
	}

	// Validate against tool names
	if !isValidToolName(name, toolNames) {
		return nil, false
	}

	// Extract arguments
	argKeys := []string{"arguments", "parameters", "input", "args"}
	var args map[string]interface{}
	for _, key := range argKeys {
		if val, ok := result[key].(map[string]interface{}); ok {
			args = val
			break
		}
	}

	if args == nil {
		args = make(map[string]interface{})
	}

	return []map[string]interface{}{
		{
			"name":      name,
			"arguments": args,
		},
	}, true
}

// parseToolCallsFromCodeBlock parses tool calls from a code block
func parseToolCallsFromCodeBlock(content string, toolNames []string) ([]map[string]interface{}, error) {
	// Find code block
	start := strings.Index(content, "```")
	if start == -1 {
		return nil, nil
	}

	end := strings.Index(content[start+3:], "```")
	if end == -1 {
		return nil, nil
	}

	codeContent := content[start+3 : start+3+end]
	codeContent = strings.TrimSpace(codeContent)

	// Remove language identifier if present
	if idx := strings.Index(codeContent, "\n"); idx != -1 {
		codeContent = strings.TrimSpace(codeContent[idx+1:])
	}

	return TryParseToolCalls(codeContent, toolNames)
}

// parseToolCallsFromXML parses tool calls from XML tags
func parseToolCallsFromXML(content string, toolNames []string) ([]map[string]interface{}, error) {
	var toolCalls []map[string]interface{}

	// Find all <invoke>...</invoke> blocks
	start := 0
	for {
		invokeStart := strings.Index(content[start:], "<invoke>")
		if invokeStart == -1 {
			break
		}
		invokeStart += start

		invokeEnd := strings.Index(content[invokeStart:], "</invoke>")
		if invokeEnd == -1 {
			break
		}
		invokeEnd += invokeStart + len("</invoke>")

		invokeContent := content[invokeStart+len("<invoke>") : invokeEnd-len("</invoke>")]

		// Parse name
		nameStart := strings.Index(invokeContent, "<name>")
		nameEnd := strings.Index(invokeContent, "</name>")
		if nameStart == -1 || nameEnd == -1 {
			start = invokeEnd
			continue
		}

		name := strings.TrimSpace(invokeContent[nameStart+len("<name>") : nameEnd])

		// Validate tool name
		if !isValidToolName(name, toolNames) {
			start = invokeEnd
			continue
		}

		// Parse arguments
		argsStart := strings.Index(invokeContent, "<arguments>")
		argsEnd := strings.Index(invokeContent, "</arguments>")
		var args map[string]interface{}

		if argsStart != -1 && argsEnd != -1 {
			argsContent := strings.TrimSpace(invokeContent[argsStart+len("<arguments>") : argsEnd])
			args = make(map[string]interface{})
			json.Unmarshal([]byte(argsContent), &args)
		}

		toolCalls = append(toolCalls, map[string]interface{}{
			"name":      name,
			"arguments": args,
		})

		start = invokeEnd
	}

	if len(toolCalls) == 0 {
		return nil, nil
	}

	return toolCalls, nil
}

// parseToolCallsFromFunctionSyntax parses tool calls from function syntax
func parseToolCallsFromFunctionSyntax(content string, toolNames []string) ([]map[string]interface{}, error) {
	var toolCalls []map[string]interface{}

	// Find function calls like functionName({...})
	start := 0
	for {
		// Find function name start
		fnStart := -1
		for i := start; i < len(content); i++ {
			if content[i] >= 'a' && content[i] <= 'z' || content[i] >= 'A' && content[i] <= 'Z' || content[i] == '_' {
				fnStart = i
				break
			}
		}

		if fnStart == -1 {
			break
		}

		// Find function name end
		fnEnd := fnStart
		for fnEnd < len(content) {
			c := content[fnEnd]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				break
			}
			fnEnd++
		}

		name := content[fnStart:fnEnd]

		// Validate tool name
		if !isValidToolName(name, toolNames) {
			start = fnEnd
			continue
		}

		// Find opening parenthesis
		argsStart := strings.Index(content[fnEnd:], "(")
		if argsStart == -1 {
			break
		}
		argsStart += fnEnd

		// Find closing parenthesis
		argsEnd := findMatchingParen(content, argsStart)
		if argsEnd == -1 {
			break
		}

		argsContent := strings.TrimSpace(content[argsStart+1 : argsEnd])

		// Parse arguments as JSON
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsContent), &args); err != nil {
			// Try parsing as simple key=value pairs
			args = parseSimpleArgs(argsContent)
		}

		toolCalls = append(toolCalls, map[string]interface{}{
			"name":      name,
			"arguments": args,
		})

		start = argsEnd + 1
	}

	if len(toolCalls) == 0 {
		return nil, nil
	}

	return toolCalls, nil
}

// findMatchingParen finds the matching closing parenthesis
func findMatchingParen(content string, start int) int {
	depth := 1
	for i := start + 1; i < len(content); i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseSimpleArgs parses simple key=value arguments
func parseSimpleArgs(content string) map[string]interface{} {
	args := make(map[string]interface{})

	pairs := strings.Split(content, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if idx := strings.Index(pair, "="); idx != -1 {
			key := strings.TrimSpace(pair[:idx])
			value := strings.TrimSpace(pair[idx+1:])
			args[key] = value
		}
	}

	return args
}

// isValidToolName checks if a tool name is in the allowed list
func isValidToolName(name string, toolNames []string) bool {
	if len(toolNames) == 0 {
		return true
	}

	for _, toolName := range toolNames {
		if strings.EqualFold(name, toolName) {
			return true
		}
	}
	return false
}

// InjectToolCalls replaces text content with tool_use blocks
func InjectToolCalls(toolCalls []map[string]interface{}) ([]types.AContentBlock, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	var result []types.AContentBlock

	for i, tc := range toolCalls {
		name, _ := tc["name"].(string)
		args, _ := tc["arguments"].(map[string]interface{})
		inputJSON, _ := json.Marshal(args)

		result = append(result, types.AContentBlock{
			Type:  "tool_use",
			ID:    fmt.Sprintf("toolu_%d", i),
			Name:  name,
			Input: json.RawMessage(inputJSON),
		})
	}

	return result, nil
}
