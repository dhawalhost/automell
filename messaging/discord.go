package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// LLMCaller is satisfied by proxy.LLMClient
type LLMCaller interface {
	Chat(message string) (string, error)
	ChatCtx(ctx context.Context, message string) (string, error)
}

// DiscordBot represents a Discord bot
type DiscordBot struct {
	token       string
	channelID   string
	llm         LLMCaller
	httpClient  *http.Client
	wsConn      *websocket.Conn
	quitChan    chan struct{}
	store       *SessionStore
	runCtx      context.Context
	transcriber Transcriber
}

// NewDiscordBot creates a new Discord bot.
// llm can be nil — messages will be logged but not forwarded to the LLM.
func NewDiscordBot(token, channelID string, llm LLMCaller) *DiscordBot {
	return &DiscordBot{
		token:      token,
		channelID:  channelID,
		llm:        llm,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		quitChan:   make(chan struct{}),
		store:      NewSessionStore(),
	}
}

// SetTranscriber configures the voice transcription backend.
func (b *DiscordBot) SetTranscriber(t Transcriber) {
	b.transcriber = t
}

// Start starts the Discord bot and connects to the Gateway.
func (b *DiscordBot) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	b.runCtx = ctx
	defer cancel()

	wsURL := "wss://gateway.discord.gg/?v=10&encoding=json"
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Discord Gateway: %w", err)
	}
	b.wsConn = conn
	defer conn.Close()

	for {
		select {
		case <-b.quitChan:
			return nil
		default:
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return fmt.Errorf("failed to read message: %w", err)
			}
			if err := b.handleMessage(msg); err != nil {
				log.Printf("Discord: error handling message: %v", err)
			}
		}
	}
}

// Stop stops the Discord bot.
func (b *DiscordBot) Stop() {
	close(b.quitChan)
	if b.wsConn != nil {
		b.wsConn.Close()
	}
}

func (b *DiscordBot) handleMessage(msg map[string]interface{}) error {
	op, ok := msg["op"].(float64)
	if !ok {
		return nil
	}
	switch int(op) {
	case 0:
		return b.handleDispatch(msg)
	case 10:
		return b.handleHello(msg)
	}
	return nil
}

func (b *DiscordBot) handleDispatch(msg map[string]interface{}) error {
	eventType, _ := msg["t"].(string)
	data, _ := msg["d"].(map[string]interface{})
	switch eventType {
	case "MESSAGE_CREATE":
		return b.handleMessageCreate(data)
	case "READY":
		log.Println("Discord bot ready")
	}
	return nil
}

func (b *DiscordBot) handleHello(msg map[string]interface{}) error {
	data, _ := msg["d"].(map[string]interface{})
	heartbeatInterval, _ := data["heartbeat_interval"].(float64)

	identify := map[string]interface{}{
		"op": 2,
		"d": map[string]interface{}{
			"token": b.token,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "automell",
				"device":  "automell",
			},
			"intents": 1 << 9,
		},
	}
	if err := b.wsConn.WriteJSON(identify); err != nil {
		return fmt.Errorf("failed to send identify: %w", err)
	}
	go b.startHeartbeat(time.Duration(heartbeatInterval) * time.Millisecond)
	return nil
}

func (b *DiscordBot) handleMessageCreate(data map[string]interface{}) error {
	channelID, _ := data["channel_id"].(string)
	if channelID != b.channelID {
		return nil
	}
	author, _ := data["author"].(map[string]interface{})
	if bot, _ := author["bot"].(bool); bot {
		return nil
	}
	content, _ := data["content"].(string)
	msgID, _ := data["id"].(string)

	replyToID := ""
	if ref, ok := data["referenced_message"].(map[string]interface{}); ok {
		replyToID, _ = ref["id"].(string)
	}

	chatID := b.channelID
	trimmed := strings.TrimSpace(content)

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

	// Handle voice / audio attachments when transcription is enabled
	if b.transcriber != nil && content == "" {
		attachments, _ := data["attachments"].([]interface{})
		for _, raw := range attachments {
			att, _ := raw.(map[string]interface{})
			url, _ := att["url"].(string)
			ct, _ := att["content_type"].(string)
			filename, _ := att["filename"].(string)
			if url == "" {
				continue
			}
			if isAudioAttachment(ct, filename) {
				b.SendMessage("🎙 Transcribing...")
				audioData, dlErr := b.downloadBytes(url)
				if dlErr != nil {
					log.Printf("Discord: download attachment: %v", dlErr)
					continue
				}
				text, txErr := b.transcriber.Transcribe(context.Background(), audioData, ct)
				if txErr != nil {
					log.Printf("Discord: transcription error: %v", txErr)
					b.SendMessage("⚠️ Transcription failed: " + txErr.Error())
					continue
				}
				content = text
				break
			}
		}
	}

	if content == "" {
		return nil
	}

	if b.llm == nil {
		log.Printf("Discord: received message (no LLM configured): %s", content)
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
		reply, err := b.llm.ChatCtx(sessCtx, content)
		if err != nil {
			if sessCtx.Err() != nil {
				log.Printf("Discord: session %s cancelled", sess.ID)
				return
			}
			log.Printf("Discord: LLM error: %v", err)
			b.SendMessage("⚠️ Error: " + err.Error())
			b.store.MarkDone(sess.ID)
			return
		}
		b.store.MarkDone(sess.ID)
		if err := b.SendMessage(reply); err != nil {
			log.Printf("Discord: failed to send reply: %v", err)
		}
	}()
	return nil
}

// downloadBytes fetches a URL and returns the body as bytes.
func (b *DiscordBot) downloadBytes(url string) ([]byte, error) {
	resp, err := b.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// isAudioAttachment returns true if the content type or filename indicates audio.
func isAudioAttachment(contentType, filename string) bool {
	for _, kw := range []string{"audio", "ogg", "mp3", "mpeg", "wav", "flac", "m4a", "webm"} {
		if strings.Contains(strings.ToLower(contentType), kw) {
			return true
		}
	}
	for _, ext := range []string{".ogg", ".mp3", ".m4a", ".wav", ".flac", ".webm", ".opus"} {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return true
		}
	}
	return false
}

func (b *DiscordBot) startHeartbeat(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.quitChan:
			return
		case <-ticker.C:
			heartbeat := map[string]interface{}{"op": 1, "d": nil}
			if err := b.wsConn.WriteJSON(heartbeat); err != nil {
				log.Printf("Discord: heartbeat error: %v", err)
				return
			}
		}
	}
}

// SendMessage sends a message to the configured channel.
func (b *DiscordBot) SendMessage(text string) error {
	payload := map[string]string{"content": text}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	apiURL := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", b.channelID)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord API error: status %d", resp.StatusCode)
	}
	return nil
}
