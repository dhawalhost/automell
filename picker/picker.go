package picker

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// RunInteractivePicker runs an interactive model picker in the terminal
func RunInteractivePicker(models []string) (string, error) {
	if len(models) == 0 {
		return "", fmt.Errorf("no models available")
	}

	fmt.Println("Available models:")
	for i, model := range models {
		fmt.Printf("  [%d] %s\n", i+1, model)
	}

	fmt.Print("\nSelect model (number or name): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)

	// Check if input is a number
	var index int
	if _, err := fmt.Sscanf(input, "%d", &index); err == nil {
		if index >= 1 && index <= len(models) {
			return models[index-1], nil
		}
		return "", fmt.Errorf("invalid selection: %d", index)
	}

	// Check if input is a model name
	for _, model := range models {
		if strings.EqualFold(input, model) {
			return model, nil
		}
	}

	return "", fmt.Errorf("invalid model name: %s", input)
}

// DisplayModelInfo displays information about a model
func DisplayModelInfo(model string) {
	fmt.Printf("Selected model: %s\n", model)
	fmt.Println("\nTo use this model with Claude Code:")
	fmt.Printf("  export ANTHROPIC_BASE_URL=\"http://localhost:8082\"\n")
	fmt.Printf("  export ANTHROPIC_AUTH_TOKEN=\"your-token\"\n")
	fmt.Printf("  claude\n")
}
