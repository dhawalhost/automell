package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhawalhost/automell/cli"
	"github.com/dhawalhost/automell/config"
	"github.com/dhawalhost/automell/messaging"
	"github.com/dhawalhost/automell/picker"
	"github.com/dhawalhost/automell/proxy"
)

// proxyBaseURL returns the local proxy base URL for the given port
func proxyBaseURL(port string) string {
	return "http://localhost:" + port
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create CLI
	c := cli.NewCLI()

	// Register command handlers
	c.RegisterHandler("serve", func() error {
		return serveCommand(cfg)
	})

	c.RegisterHandler("pick", func() error {
		return pickCommand(cfg)
	})

	c.RegisterHandler("bot", func() error {
		return botCommand(cfg)
	})

	// Parse and execute command
	if err := c.Parse(); err != nil {
		log.Fatal(err)
	}
}

// serveCommand starts the HTTP proxy server
func serveCommand(cfg *config.Config) error {
	// Create proxy server
	server := proxy.NewServer(cfg)

	// Validate provider configuration at startup
	if err := proxy.ValidateConfig(cfg); err != nil {
		log.Printf("[WARN] Configuration issue: %v", err)
	}

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: server,
	}

	// Bind the port before launching the goroutine so we can log "ready" only
	// after the socket is actually listening and requests will be accepted.
	ln, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("failed to bind port %s: %w", cfg.Port, err)
	}
	log.Printf("automell listening on http://localhost:%s — ready", cfg.Port)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down server...")

	// Cancel all in-flight streaming provider requests first so the HTTP server
	// can drain connections quickly instead of waiting for generation to finish.
	server.Shutdown()

	// Graceful shutdown — allow a short window for non-streaming requests to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	log.Println("Server stopped")
	return nil
}

// pickCommand runs the interactive model picker
func pickCommand(cfg *config.Config) error {
	// Define available models
	models := []string{
		"nvidia_nim/meta/llama-3.1-405b-instruct",
		"nvidia_nim/meta/llama-3.1-70b-instruct",
		"nvidia_nim/nvidia/llama-3.1-nemotron-70b-instruct",
		"open_router/anthropic/claude-3.5-sonnet",
		"open_router/openai/gpt-4o",
		"deepseek/deepseek-chat",
		"lmstudio/local-model",
		"llamacpp/local-model",
	}

	// Run interactive picker
	selectedModel, err := picker.RunInteractivePicker(models)
	if err != nil {
		return fmt.Errorf("picker error: %w", err)
	}

	// Display model info
	picker.DisplayModelInfo(selectedModel)

	// Update config with selected model
	cfg.Model = selectedModel

	return nil
}

// botCommand starts the messaging bot
func botCommand(cfg *config.Config) error {
	// Determine which bot to start based on available config
	if cfg.DiscordBotToken != "" && cfg.DiscordChannelID != "" {
		return startDiscordBot(cfg)
	} else if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		return startTelegramBot(cfg)
	} else {
		return fmt.Errorf("no bot configuration found. Set DISCORD_BOT_TOKEN/DISCORD_CHANNEL_ID or TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID")
	}
}

// startDiscordBot starts the Discord bot
func startDiscordBot(cfg *config.Config) error {
	llm := proxy.NewLLMClient(proxyBaseURL(cfg.Port), cfg.AnthropicAuthToken, cfg.Model)
	bot := messaging.NewDiscordBot(cfg.DiscordBotToken, cfg.DiscordChannelID, llm)

	if t, err := messaging.NewTranscriber(cfg); err != nil {
		log.Printf("[WARN] voice transcription disabled: %v", err)
	} else if t != nil {
		bot.SetTranscriber(t)
		log.Println("Voice transcription enabled for Discord")
	}

	log.Println("Starting Discord bot...")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := bot.Start(); err != nil {
			log.Printf("Discord bot error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down Discord bot...")
	bot.Stop()

	return nil
}

// startTelegramBot starts the Telegram bot
func startTelegramBot(cfg *config.Config) error {
	llm := proxy.NewLLMClient(proxyBaseURL(cfg.Port), cfg.AnthropicAuthToken, cfg.Model)
	bot, err := messaging.NewTelegramBot(cfg.TelegramBotToken, cfg.TelegramChatID, llm)
	if err != nil {
		return fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	if t, err := messaging.NewTranscriber(cfg); err != nil {
		log.Printf("[WARN] voice transcription disabled: %v", err)
	} else if t != nil {
		bot.SetTranscriber(t)
		log.Println("Voice transcription enabled for Telegram")
	}

	log.Println("Starting Telegram bot...")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := bot.Start(); err != nil {
			log.Printf("Telegram bot error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down Telegram bot...")
	bot.Stop()

	return nil
}
