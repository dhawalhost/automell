package messaging

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramBot represents a Telegram bot
type TelegramBot struct {
	bot         *tgbotapi.BotAPI
	chatID      int64
	llm         LLMCaller
	quitChan    chan struct{}
	store       *SessionStore
	runCtx      context.Context
	transcriber Transcriber
}

// NewTelegramBot creates a new Telegram bot
// llm can be nil — messages will be logged but not forwarded to the LLM
func NewTelegramBot(token, chatID string, llm LLMCaller) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}
	bot.Debug = false

	parsedChatID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid chat ID: %w", err)
	}

	return &TelegramBot{
		bot:      bot,
		chatID:   parsedChatID,
		llm:      llm,
		quitChan: make(chan struct{}),
		store:    NewSessionStore(),
	}, nil
}

// Start starts polling for updates
func (b *TelegramBot) Start() error {
	log.Printf("Telegram bot started (chat ID: %d)", b.chatID)

	ctx, cancel := context.WithCancel(context.Background())
	b.runCtx = ctx
	defer cancel()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.bot.GetUpdatesChan(u)

	for {
		select {
		case <-b.quitChan:
			b.bot.StopReceivingUpdates()
			return nil
		case update := <-updates:
			if update.Message != nil && update.Message.Chat.ID == b.chatID {
				if err := b.handleMessage(update.Message); err != nil {
					log.Printf("Telegram: error handling message: %v", err)
				}
			}
		}
	}
}

// Stop stops the bot
func (b *TelegramBot) Stop() {
	close(b.quitChan)
	b.bot.StopReceivingUpdates()
}

// SetTranscriber configures the voice transcription backend.
func (b *TelegramBot) SetTranscriber(t Transcriber) {
	b.transcriber = t
}

// downloadURL fetches raw bytes from a URL using the default HTTP client.
func downloadURL(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec // URL comes from Telegram API, not user input
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// handleMessage processes an incoming message and replies via the LLM
func (b *TelegramBot) handleMessage(message *tgbotapi.Message) error {
	if message.From != nil && message.From.IsBot {
		return nil
	}
	text := message.Text
	chatID := strconv.FormatInt(b.chatID, 10)
	msgID := strconv.Itoa(message.MessageID)

	// Resolve reply-to message ID for branch sessions
	replyToID := ""
	if message.ReplyToMessage != nil {
		replyToID = strconv.Itoa(message.ReplyToMessage.MessageID)
	}

	// Handle slash commands
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "/stop":
		var count int
		if replyToID != "" {
			if sess := b.store.ResolveParentSession(replyToID); sess != nil {
				if b.store.Cancel(sess.ID) {
					count = 1
				}
			}
		} else {
			count = b.store.CancelAll(chatID)
		}
		noun := "request"
		if count != 1 {
			noun = "requests"
		}
		b.SendMessage(fmt.Sprintf("⏹ Stopped. Cancelled %d %s.", count, noun))
		return nil

	case trimmed == "/clear":
		if replyToID != "" {
			if sess := b.store.ResolveParentSession(replyToID); sess != nil {
				b.store.Cancel(sess.ID)
			}
		} else {
			b.store.Clear(chatID)
		}
		b.SendMessage("🗑 Cleared.")
		return nil

	case trimmed == "/stats":
		b.SendMessage(b.store.Stats(chatID))
		return nil
	}

	if text == "" && b.transcriber != nil {
		// Check for voice/audio messages
		var fileID, mimeType string
		if message.Voice != nil {
			fileID = message.Voice.FileID
			mimeType = "audio/ogg"
		} else if message.Audio != nil {
			fileID = message.Audio.FileID
			mimeType = message.Audio.MimeType
		}
		if fileID != "" {
			b.SendMessage("\U0001F399 Transcribing...")
			url, err := b.bot.GetFileDirectURL(fileID)
			if err != nil {
				log.Printf("Telegram: get file URL: %v", err)
			} else {
				audioData, err := downloadURL(url)
				if err != nil {
					log.Printf("Telegram: download audio: %v", err)
				} else {
					tx, err := b.transcriber.Transcribe(context.Background(), audioData, mimeType)
					if err != nil {
						log.Printf("Telegram: transcription error: %v", err)
						b.SendMessage("\u26a0\ufe0f Transcription failed: " + err.Error())
					} else {
						text = tx
					}
				}
			}
		}
	}

	if text == "" {
		return nil
	}

	if b.llm == nil {
		log.Printf("Telegram: received message (no LLM configured): %s", text)
		return nil
	}

	parentID := ""
	if replyToID != "" {
		if parent := b.store.ResolveParentSession(replyToID); parent != nil {
			parentID = parent.ID
		}
	}

	sess, sessCtx := b.store.Create(b.runCtx, chatID, msgID, parentID)

	go func() {
		sess.RequestCount++
		reply, err := b.llm.ChatCtx(sessCtx, text)
		if err != nil {
			if sessCtx.Err() != nil {
				log.Printf("Telegram: session %s cancelled", sess.ID)
				return
			}
			log.Printf("Telegram: LLM error: %v", err)
			b.SendMessage("⚠️ Error: " + err.Error())
			b.store.MarkDone(sess.ID)
			return
		}
		b.store.MarkDone(sess.ID)
		if err := b.SendMarkdown(reply); err != nil {
			// Fall back to plain text
			b.SendMessage(reply)
		}
	}()
	return nil
}

// SendMessage sends a plain-text message to the configured chat
func (b *TelegramBot) SendMessage(text string) error {
	msg := tgbotapi.NewMessage(b.chatID, text)
	_, err := b.bot.Send(msg)
	return err
}

// SendMarkdown sends a Markdown-formatted message to the configured chat
func (b *TelegramBot) SendMarkdown(text string) error {
	msg := tgbotapi.NewMessage(b.chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	_, err := b.bot.Send(msg)
	return err
}
