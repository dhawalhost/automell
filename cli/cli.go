package cli

import (
	"flag"
	"fmt"
	"os"
)

// Command represents a CLI command
type Command struct {
	Name        string
	Description string
	Handler     func() error
}

// CLI represents the command-line interface
type CLI struct {
	commands []Command
}

// NewCLI creates a new CLI instance
func NewCLI() *CLI {
	return &CLI{
		commands: []Command{
			{
				Name:        "serve",
				Description: "Start the HTTP proxy server",
				Handler:     nil, // Set by main
			},
			{
				Name:        "pick",
				Description: "Interactive model picker",
				Handler:     nil, // Set by main
			},
			{
				Name:        "bot",
				Description: "Start messaging bot (discord/telegram)",
				Handler:     nil, // Set by main
			},
		},
	}
}

// RegisterHandler registers a handler for a command
func (c *CLI) RegisterHandler(name string, handler func() error) {
	for i, cmd := range c.commands {
		if cmd.Name == name {
			c.commands[i].Handler = handler
			return
		}
	}
}

// Parse parses command-line arguments and executes the appropriate command
func (c *CLI) Parse() error {
	if len(os.Args) < 2 {
		c.printUsage()
		return fmt.Errorf("no command specified")
	}

	commandName := os.Args[1]

	// Create a flag set for the command
	fs := flag.NewFlagSet(commandName, flag.ExitOnError)
	fs.Usage = func() {
		c.printCommandUsage(commandName)
	}

	// Parse flags for the command
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	// Find and execute the command
	for _, cmd := range c.commands {
		if cmd.Name == commandName {
			if cmd.Handler == nil {
				return fmt.Errorf("command '%s' not implemented", commandName)
			}
			return cmd.Handler()
		}
	}

	return fmt.Errorf("unknown command: %s", commandName)
}

// printUsage prints the general usage information
func (c *CLI) printUsage() {
	fmt.Println("claugo - Claude Code proxy to free LLM providers")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  claugo <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	for _, cmd := range c.commands {
		fmt.Printf("  %-10s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  PORT                 Server port (default: 8082)")
	fmt.Println("  ANTHROPIC_AUTH_TOKEN Auth token for incoming requests")
	fmt.Println("  MODEL_OPUS           Model for Opus requests")
	fmt.Println("  MODEL_SONNET         Model for Sonnet requests")
	fmt.Println("  MODEL_HAIKU          Model for Haiku requests")
	fmt.Println("  MODEL                Default model")
	fmt.Println("  NVIDIA_NIM_API_KEY   API key for NVIDIA NIM")
	fmt.Println("  OPENROUTER_API_KEY   API key for OpenRouter")
	fmt.Println("  DEEPSEEK_API_KEY     API key for DeepSeek")
	fmt.Println("  RATE_LIMIT_RPM       Rate limit per minute")
	fmt.Println("  RATE_LIMIT_RPD       Rate limit per day")
	fmt.Println("  CONCURRENCY_LIMIT    Max concurrent requests")
}

// printCommandUsage prints usage for a specific command
func (c *CLI) printCommandUsage(commandName string) {
	fmt.Printf("claugo %s\n\n", commandName)

	for _, cmd := range c.commands {
		if cmd.Name == commandName {
			fmt.Printf("Description: %s\n", cmd.Description)
			break
		}
	}

	fmt.Println("\nOptions:")
	fmt.Println("  -h, --help  Show this help message")
}
