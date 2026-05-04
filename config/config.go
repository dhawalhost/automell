package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dhawalhost/automell/dotenv"
)

var (
	cfg     *Config
	once    sync.Once
	loadErr error
)

// Config holds all configuration values
type Config struct {
	// Server settings
	Port               string
	AnthropicAuthToken string

	// Model routing
	ModelOpus   string
	ModelSonnet string
	ModelHaiku  string
	Model       string

	// Provider API keys
	NvidiaNimAPIKey  string
	OpenRouterAPIKey string
	DeepSeekAPIKey   string

	// Per-provider base URLs (for local providers)
	LMStudioBaseURL string
	LlamaCppBaseURL string
	OllamaBaseURL   string

	// Per-provider HTTP/SOCKS5 proxies
	NvidiaNimProxy  string
	OpenRouterProxy string
	LMStudioProxy   string
	LlamaCppProxy   string
	OllamaProxy     string

	// Thinking output toggles
	// EnableModelThinking is the global default; per-model overrides use "true"/"false"/"" (empty = inherit).
	EnableModelThinking  bool
	EnableOpusThinking   string // "true", "false", or "" (inherit global)
	EnableSonnetThinking string
	EnableHaikuThinking  string

	// Rate limiting
	RateLimitRPM     int
	RateLimitRPD     int
	ConcurrencyLimit int

	// HTTP client timeouts (seconds)
	HTTPReadTimeoutS           int // upstream read/response timeout
	HTTPWriteTimeoutS          int // upstream write/request timeout
	HTTPConnectTimeoutS        int // dial+TLS timeout
	HTTPResponseHeaderTimeoutS int // max wait for first response header byte

	// Debug/diagnostic flags
	LogRawAPIPayloads bool
	LogRawSSEEvents   bool

	// Web server tools
	EnableWebServerTools         bool
	WebFetchAllowedSchemes       []string // default: ["http","https"]
	WebFetchAllowPrivateNetworks bool     // default: false

	// Voice transcription
	VoiceNoteEnabled bool
	WhisperDevice    string // cpu | cuda | nvidia_nim
	WhisperModel     string // e.g. "base"
	HFToken          string

	// Startup smoke tests (opt-in)
	SmokeTestOnStartup bool

	// Messaging session
	MessagingSessionTimeoutS int // seconds before idle session is reaped

	// Messaging
	DiscordBotToken  string
	DiscordChannelID string
	TelegramBotToken string
	TelegramChatID   string
}

// Load loads configuration from .env file and environment variables
// Environment variables take precedence over .env values
func Load() (*Config, error) {
	once.Do(func() {
		cfg = &Config{
			Port:                       "8082",
			AnthropicAuthToken:         "",
			ModelOpus:                  "claude-3-5-sonnet-20241022",
			ModelSonnet:                "claude-3-5-sonnet-20241022",
			ModelHaiku:                 "claude-3-5-haiku-20241022",
			Model:                      "claude-3-5-sonnet-20241022",
			LMStudioBaseURL:            "http://localhost:1234/v1",
			LlamaCppBaseURL:            "http://localhost:8080/v1",
			OllamaBaseURL:              "http://localhost:11434",
			EnableModelThinking:        true,
			RateLimitRPM:               60,
			RateLimitRPD:               1000,
			ConcurrencyLimit:           5,
			HTTPReadTimeoutS:           300,
			HTTPWriteTimeoutS:          10,
			HTTPConnectTimeoutS:        10,
			HTTPResponseHeaderTimeoutS: 120,
			WebFetchAllowedSchemes:     []string{"http", "https"},
			WhisperDevice:              "cpu",
			WhisperModel:               "base",
			MessagingSessionTimeoutS:   300,
		}

		// Try to load .env file
		envMap, err := dotenv.Parse(".env")
		if err == nil {
			// Only set values that aren't already set in environment
			cfg.setFromMap(envMap, false)
		}

		// Set from environment (overwrites .env)
		cfg.setFromMap(map[string]string{
			"PORT":                             os.Getenv("PORT"),
			"ANTHROPIC_AUTH_TOKEN":             os.Getenv("ANTHROPIC_AUTH_TOKEN"),
			"MODEL_OPUS":                       os.Getenv("MODEL_OPUS"),
			"MODEL_SONNET":                     os.Getenv("MODEL_SONNET"),
			"MODEL_HAIKU":                      os.Getenv("MODEL_HAIKU"),
			"MODEL":                            os.Getenv("MODEL"),
			"NVIDIA_NIM_API_KEY":               os.Getenv("NVIDIA_NIM_API_KEY"),
			"OPENROUTER_API_KEY":               os.Getenv("OPENROUTER_API_KEY"),
			"DEEPSEEK_API_KEY":                 os.Getenv("DEEPSEEK_API_KEY"),
			"LM_STUDIO_BASE_URL":               os.Getenv("LM_STUDIO_BASE_URL"),
			"LLAMACPP_BASE_URL":                os.Getenv("LLAMACPP_BASE_URL"),
			"OLLAMA_BASE_URL":                  os.Getenv("OLLAMA_BASE_URL"),
			"NVIDIA_NIM_PROXY":                 os.Getenv("NVIDIA_NIM_PROXY"),
			"OPENROUTER_PROXY":                 os.Getenv("OPENROUTER_PROXY"),
			"LMSTUDIO_PROXY":                   os.Getenv("LMSTUDIO_PROXY"),
			"LLAMACPP_PROXY":                   os.Getenv("LLAMACPP_PROXY"),
			"OLLAMA_PROXY":                     os.Getenv("OLLAMA_PROXY"),
			"ENABLE_MODEL_THINKING":            os.Getenv("ENABLE_MODEL_THINKING"),
			"ENABLE_OPUS_THINKING":             os.Getenv("ENABLE_OPUS_THINKING"),
			"ENABLE_SONNET_THINKING":           os.Getenv("ENABLE_SONNET_THINKING"),
			"ENABLE_HAIKU_THINKING":            os.Getenv("ENABLE_HAIKU_THINKING"),
			"RATE_LIMIT_RPM":                   os.Getenv("RATE_LIMIT_RPM"),
			"RATE_LIMIT_RPD":                   os.Getenv("RATE_LIMIT_RPD"),
			"CONCURRENCY_LIMIT":                os.Getenv("CONCURRENCY_LIMIT"),
			"HTTP_READ_TIMEOUT":                os.Getenv("HTTP_READ_TIMEOUT"),
			"HTTP_WRITE_TIMEOUT":               os.Getenv("HTTP_WRITE_TIMEOUT"),
			"HTTP_CONNECT_TIMEOUT":             os.Getenv("HTTP_CONNECT_TIMEOUT"),
			"LOG_RAW_API_PAYLOADS":             os.Getenv("LOG_RAW_API_PAYLOADS"),
			"LOG_RAW_SSE_EVENTS":               os.Getenv("LOG_RAW_SSE_EVENTS"),
			"ENABLE_WEB_SERVER_TOOLS":          os.Getenv("ENABLE_WEB_SERVER_TOOLS"),
			"WEB_FETCH_ALLOWED_SCHEMES":        os.Getenv("WEB_FETCH_ALLOWED_SCHEMES"),
			"WEB_FETCH_ALLOW_PRIVATE_NETWORKS": os.Getenv("WEB_FETCH_ALLOW_PRIVATE_NETWORKS"),
			"VOICE_NOTE_ENABLED":               os.Getenv("VOICE_NOTE_ENABLED"),
			"WHISPER_DEVICE":                   os.Getenv("WHISPER_DEVICE"),
			"WHISPER_MODEL":                    os.Getenv("WHISPER_MODEL"),
			"HF_TOKEN":                         os.Getenv("HF_TOKEN"),
			"SMOKE_TEST_ON_STARTUP":            os.Getenv("SMOKE_TEST_ON_STARTUP"),
			"MESSAGING_SESSION_TIMEOUT":        os.Getenv("MESSAGING_SESSION_TIMEOUT"),
			"DISCORD_BOT_TOKEN":                os.Getenv("DISCORD_BOT_TOKEN"),
			"DISCORD_CHANNEL_ID":               os.Getenv("DISCORD_CHANNEL_ID"),
			"TELEGRAM_BOT_TOKEN":               os.Getenv("TELEGRAM_BOT_TOKEN"),
			"TELEGRAM_CHAT_ID":                 os.Getenv("TELEGRAM_CHAT_ID"),
		}, true)
	})

	return cfg, loadErr
}

func (c *Config) setFromMap(m map[string]string, overwrite bool) {
	if overwrite || c.Port == "8082" {
		if v := m["PORT"]; v != "" {
			c.Port = v
		}
	}
	if overwrite || c.AnthropicAuthToken == "" {
		if v := m["ANTHROPIC_AUTH_TOKEN"]; v != "" {
			c.AnthropicAuthToken = v
		}
	}
	if overwrite || c.ModelOpus == "claude-3-5-sonnet-20241022" {
		if v := m["MODEL_OPUS"]; v != "" {
			c.ModelOpus = v
		}
	}
	if overwrite || c.ModelSonnet == "claude-3-5-sonnet-20241022" {
		if v := m["MODEL_SONNET"]; v != "" {
			c.ModelSonnet = v
		}
	}
	if overwrite || c.ModelHaiku == "claude-3-5-haiku-20241022" {
		if v := m["MODEL_HAIKU"]; v != "" {
			c.ModelHaiku = v
		}
	}
	if overwrite || c.Model == "claude-3-5-sonnet-20241022" {
		if v := m["MODEL"]; v != "" {
			c.Model = v
		}
	}
	if overwrite || c.NvidiaNimAPIKey == "" {
		if v := m["NVIDIA_NIM_API_KEY"]; v != "" {
			c.NvidiaNimAPIKey = v
		}
	}
	if overwrite || c.OpenRouterAPIKey == "" {
		if v := m["OPENROUTER_API_KEY"]; v != "" {
			c.OpenRouterAPIKey = v
		}
	}
	if overwrite || c.DeepSeekAPIKey == "" {
		if v := m["DEEPSEEK_API_KEY"]; v != "" {
			c.DeepSeekAPIKey = v
		}
	}
	if overwrite || c.LMStudioBaseURL == "http://localhost:1234/v1" {
		if v := m["LM_STUDIO_BASE_URL"]; v != "" {
			c.LMStudioBaseURL = v
		}
	}
	if overwrite || c.LlamaCppBaseURL == "http://localhost:8080/v1" {
		if v := m["LLAMACPP_BASE_URL"]; v != "" {
			c.LlamaCppBaseURL = v
		}
	}
	if overwrite || c.OllamaBaseURL == "http://localhost:11434" {
		if v := m["OLLAMA_BASE_URL"]; v != "" {
			c.OllamaBaseURL = v
		}
	}
	if overwrite || c.NvidiaNimProxy == "" {
		if v := m["NVIDIA_NIM_PROXY"]; v != "" {
			c.NvidiaNimProxy = v
		}
	}
	if overwrite || c.OpenRouterProxy == "" {
		if v := m["OPENROUTER_PROXY"]; v != "" {
			c.OpenRouterProxy = v
		}
	}
	if overwrite || c.LMStudioProxy == "" {
		if v := m["LMSTUDIO_PROXY"]; v != "" {
			c.LMStudioProxy = v
		}
	}
	if overwrite || c.LlamaCppProxy == "" {
		if v := m["LLAMACPP_PROXY"]; v != "" {
			c.LlamaCppProxy = v
		}
	}
	if overwrite || c.OllamaProxy == "" {
		if v := m["OLLAMA_PROXY"]; v != "" {
			c.OllamaProxy = v
		}
	}
	if overwrite {
		if v := m["ENABLE_MODEL_THINKING"]; v != "" {
			c.EnableModelThinking = parseBool(v, true)
		}
	}
	// Per-model thinking overrides: store raw string so empty means "inherit"
	if v, ok := m["ENABLE_OPUS_THINKING"]; ok && v != "" {
		c.EnableOpusThinking = v
	}
	if v, ok := m["ENABLE_SONNET_THINKING"]; ok && v != "" {
		c.EnableSonnetThinking = v
	}
	if v, ok := m["ENABLE_HAIKU_THINKING"]; ok && v != "" {
		c.EnableHaikuThinking = v
	}
	if overwrite || c.RateLimitRPM == 60 {
		if v := m["RATE_LIMIT_RPM"]; v != "" {
			c.RateLimitRPM = parseInt(v, 60)
		}
	}
	if overwrite || c.RateLimitRPD == 1000 {
		if v := m["RATE_LIMIT_RPD"]; v != "" {
			c.RateLimitRPD = parseInt(v, 1000)
		}
	}
	if overwrite || c.ConcurrencyLimit == 5 {
		if v := m["CONCURRENCY_LIMIT"]; v != "" {
			c.ConcurrencyLimit = parseInt(v, 5)
		}
	}
	if overwrite || c.HTTPReadTimeoutS == 300 {
		if v := m["HTTP_READ_TIMEOUT"]; v != "" {
			c.HTTPReadTimeoutS = parseInt(v, 300)
		}
	}
	if overwrite || c.HTTPWriteTimeoutS == 10 {
		if v := m["HTTP_WRITE_TIMEOUT"]; v != "" {
			c.HTTPWriteTimeoutS = parseInt(v, 10)
		}
	}
	if overwrite || c.HTTPConnectTimeoutS == 10 {
		if v := m["HTTP_CONNECT_TIMEOUT"]; v != "" {
			c.HTTPConnectTimeoutS = parseInt(v, 10)
		}
	}
	if overwrite || c.HTTPResponseHeaderTimeoutS == 120 {
		if v := m["HTTP_RESPONSE_HEADER_TIMEOUT"]; v != "" {
			c.HTTPResponseHeaderTimeoutS = parseInt(v, 120)
		}
	}
	if overwrite {
		if v := m["LOG_RAW_API_PAYLOADS"]; v != "" {
			c.LogRawAPIPayloads = parseBool(v, false)
		}
		if v := m["LOG_RAW_SSE_EVENTS"]; v != "" {
			c.LogRawSSEEvents = parseBool(v, false)
		}
		if v := m["ENABLE_WEB_SERVER_TOOLS"]; v != "" {
			c.EnableWebServerTools = parseBool(v, false)
		}
		if v := m["WEB_FETCH_ALLOW_PRIVATE_NETWORKS"]; v != "" {
			c.WebFetchAllowPrivateNetworks = parseBool(v, false)
		}
		if v := m["VOICE_NOTE_ENABLED"]; v != "" {
			c.VoiceNoteEnabled = parseBool(v, false)
		}
		if v := m["SMOKE_TEST_ON_STARTUP"]; v != "" {
			c.SmokeTestOnStartup = parseBool(v, false)
		}
	}
	if v := m["WEB_FETCH_ALLOWED_SCHEMES"]; v != "" {
		var schemes []string
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(strings.ToLower(s))
			if s != "" {
				schemes = append(schemes, s)
			}
		}
		if len(schemes) > 0 {
			c.WebFetchAllowedSchemes = schemes
		}
	}
	if overwrite || c.WhisperDevice == "cpu" {
		if v := m["WHISPER_DEVICE"]; v != "" {
			c.WhisperDevice = v
		}
	}
	if overwrite || c.WhisperModel == "base" {
		if v := m["WHISPER_MODEL"]; v != "" {
			c.WhisperModel = v
		}
	}
	if overwrite || c.HFToken == "" {
		if v := m["HF_TOKEN"]; v != "" {
			c.HFToken = v
		}
	}
	if overwrite || c.MessagingSessionTimeoutS == 300 {
		if v := m["MESSAGING_SESSION_TIMEOUT"]; v != "" {
			c.MessagingSessionTimeoutS = parseInt(v, 300)
		}
	}
	if overwrite || c.DiscordBotToken == "" {
		if v := m["DISCORD_BOT_TOKEN"]; v != "" {
			c.DiscordBotToken = v
		}
	}
	if overwrite || c.DiscordChannelID == "" {
		if v := m["DISCORD_CHANNEL_ID"]; v != "" {
			c.DiscordChannelID = v
		}
	}
	if overwrite || c.TelegramBotToken == "" {
		if v := m["TELEGRAM_BOT_TOKEN"]; v != "" {
			c.TelegramBotToken = v
		}
	}
	if overwrite || c.TelegramChatID == "" {
		if v := m["TELEGRAM_CHAT_ID"]; v != "" {
			c.TelegramChatID = v
		}
	}
}

func parseInt(s string, defaultVal int) int {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return defaultVal
	}
	return result
}

func parseBool(s string, defaultVal bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return defaultVal
}

// ThinkingEnabledForModel returns whether extended thinking should be requested
// for the given Claude model name, respecting per-model overrides.
func (c *Config) ThinkingEnabledForModel(claudeModel string) bool {
	model := strings.ToLower(claudeModel)
	var override string
	if strings.Contains(model, "opus") {
		override = c.EnableOpusThinking
	} else if strings.Contains(model, "sonnet") {
		override = c.EnableSonnetThinking
	} else if strings.Contains(model, "haiku") {
		override = c.EnableHaikuThinking
	}
	if override == "true" || override == "1" {
		return true
	}
	if override == "false" || override == "0" {
		return false
	}
	return c.EnableModelThinking
}

// ResolveModel resolves a Claude model name to a provider model string
func (c *Config) ResolveModel(claudeModel string) string {
	claudeModel = strings.ToLower(claudeModel)

	// Check for specific model family matches
	if strings.Contains(claudeModel, "opus") {
		return c.ModelOpus
	}
	if strings.Contains(claudeModel, "sonnet") {
		return c.ModelSonnet
	}
	if strings.Contains(claudeModel, "haiku") {
		return c.ModelHaiku
	}

	// Default fallback
	return c.Model
}

// WebFetchSchemesSet returns the allowed URL schemes as a set for O(1) lookup.
func (c *Config) WebFetchSchemesSet() map[string]bool {
	m := make(map[string]bool, len(c.WebFetchAllowedSchemes))
	for _, s := range c.WebFetchAllowedSchemes {
		m[strings.ToLower(s)] = true
	}
	return m
}

// Get returns the loaded config (panics if not loaded)
func Get() *Config {
	if cfg == nil {
		panic("config not loaded, call Load() first")
	}
	return cfg
}
