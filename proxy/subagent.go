package proxy

import "encoding/json"

// forceSubagentForeground parses Task tool arguments and forces run_in_background=false.
// It returns the modified JSON or the original if parsing fails.
func forceSubagentForeground(input json.RawMessage) json.RawMessage {
	var args map[string]interface{}
	if err := json.Unmarshal(input, &args); err != nil {
		return input
	}
	if v, ok := args["run_in_background"]; ok {
		if boolVal, ok := v.(bool); ok && boolVal {
			args["run_in_background"] = false
			b, err := json.Marshal(args)
			if err != nil {
				return input
			}
			return json.RawMessage(b)
		}
	}
	return input
}
