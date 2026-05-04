# automell

> A lightweight Go proxy that translates Claude Code's Anthropic API calls to OpenAI-compatible providers — run Claude Code for free against NVIDIA NIM, OpenRouter, DeepSeek, or your own local model.

[![Go 1.21+](https://img.shields.io/badge/Go-1.21%2B-blue)](https://go.dev)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue)](LICENSE)

**Why Go?**
- Ships as a single static binary — no runtime or interpreter needed
- ~5 ms startup time vs ~2 s for Python/uvicorn
- ~10 MB memory footprint vs ~100 MB
- Native goroutines handle concurrent SSE streams efficiently

---

## How It Works

```
┌──────────────────┐       ┌──────────────────────────────┐       ┌─────────────────────┐
│   Claude Code    │──────▶│         automell             │──────▶│    LLM Provider     │
│  CLI / VS Code   │◀──────│   Proxy  (default :8082)     │◀──────│  NIM / OR / DS / …  │
└──────────────────┘       └──────────────────────────────┘       └─────────────────────┘
  Anthropic API format                                              OpenAI-compatible API
  (streaming SSE)                                                   (streaming SSE)
```

Claude Code thinks it is talking to Anthropic. automell intercepts every request, translates it to OpenAI format, forwards it to the configured provider, and streams the response back — translated to Anthropic SSE format in real time.

---

## Features

| Feature | Details |
|---|---|
| **6 providers** | NVIDIA NIM, OpenRouter, DeepSeek, LM Studio, llama.cpp, Ollama |
| **Per-model routing** | Map Opus / Sonnet / Haiku to different models and providers |
| **Thinking tokens** | `reasoning_content` / `<thinking>` tags → native Anthropic thinking blocks |
| **Full tool use** | Streaming tool-call delta translation with `input_json_delta` |
| **Subagent control** | Intercepts `Task` tool calls and forces `run_in_background=false` |
| **Request optimisation** | 5 trivial request categories (pings, title gen, suggestions…) answered locally |
| **Rate limiting** | Sliding-window RPM + RPD limiters plus a concurrency cap |
| **Auth gating** | Optional bearer-token check on every incoming request |
| **`/v1/models` endpoint** | Returns a list of supported Claude model IDs |
| **Messaging bots** | Discord and Telegram bots wired directly to the proxy |
| **Voice transcription** | Whisper-based voice note transcription for bot messages |
| **Web tools** | Enable LLMs to fetch web content and perform searches |
| **Zero runtime deps** | Single binary — `go build` is all you need |

---

## Quick Start

### 1. Prerequisites

- Go 1.21+
- [Claude Code CLI](https://github.com/anthropics/claude-code) (for Claude Code usage)
- An API key for at least one provider, **or** a locally running LM Studio / llama.cpp / Ollama

### 2. Clone & configure

```bash
git clone https://github.com/dhawalhost/automell
cd automell
cp .env.example .env
# Open .env and add your provider API key + model strings
```

### 3. Build & run

```bash
go mod download
go build -o automell .
./automell serve
```

Or run directly without building:

```bash
go run . serve
```

### 4. Point Claude Code at the proxy

```bash
# Bash / zsh
ANTHROPIC_BASE_URL="http://localhost:8082" ANTHROPIC_AUTH_TOKEN="any-token" claude

# PowerShell
$env:ANTHROPIC_BASE_URL="http://localhost:8082"; $env:ANTHROPIC_AUTH_TOKEN="any-token"; claude
```

> `ANTHROPIC_AUTH_TOKEN` can be any non-empty string when `ANTHROPIC_AUTH_TOKEN` is not set in `.env` (the proxy is open by default).

### 5. VS Code — Claude Code extension

Add to your `settings.json`:

```json
"claudeCode.environmentVariables": [
  { "name": "ANTHROPIC_BASE_URL",  "value": "http://localhost:8082" },
  { "name": "ANTHROPIC_AUTH_TOKEN", "value": "any-token" }
]
```

---

## Commands

| Command | Description |
|---|---|
| `automell serve` | Start the HTTP proxy server |
| `automell pick` | Interactive terminal model picker |
| `automell bot` | Start Discord or Telegram bot (whichever is configured) |

```bash
automell serve          # start proxy on PORT (default 8082)
automell pick           # choose a model interactively
automell bot            # start messaging bot
```

---

## Providers

| Provider | Model prefix | Cost | Notes |
|---|---|---|---|
| **NVIDIA NIM** | `nvidia_nim/` | Free — 40 req/min | Best free option; large model catalogue |
| **OpenRouter** | `open_router/` | Free & paid tiers | Hundreds of models in one API |
| **DeepSeek** | `deepseek/` | Usage-based | Excellent for reasoning/coding tasks |
| **LM Studio** | `lmstudio/` | Free (local) | GUI app; no rate limits |
| **llama.cpp** | `llamacpp/` | Free (local) | Minimal resource usage |
| **Ollama** | `ollama/` | Free (local) | Simple local model runner; broad model library |

### Model string format

Model strings use the format `<provider_prefix>/<model_name>`:

```
nvidia_nim/meta/llama-3.1-70b-instruct
^^^^^^^^^^  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
provider    model name sent to the provider API
```

You can route each Claude model family independently:

```env
MODEL_OPUS=open_router/anthropic/claude-opus-4-5
MODEL_SONNET=nvidia_nim/meta/llama-3.1-70b-instruct
MODEL_HAIKU=deepseek/deepseek-chat
MODEL=nvidia_nim/meta/llama-3.1-70b-instruct  # fallback
```

---

## Configuration

All settings are loaded from `.env` first, then overridden by real environment variables.

### Server Settings

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8082` | Port the proxy listens on |

### Model Routing

| Variable | Default | Description |
|---|---|---|
| `MODEL` | `claude-3-5-sonnet-20241022` | Fallback model (mapped via `ResolveModel`) |
| `MODEL_OPUS` | `claude-3-5-sonnet-20241022` | Provider model for `claude-opus-*` requests |
| `MODEL_SONNET` | `claude-3-5-sonnet-20241022` | Provider model for `claude-sonnet-*` requests |
| `MODEL_HAIKU` | `claude-3-5-haiku-20241022` | Provider model for `claude-haiku-*` requests |

### API Keys

| Variable | Description |
|---|---|
| `NVIDIA_NIM_API_KEY` | NVIDIA NIM API key |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `DEEPSEEK_API_KEY` | DeepSeek API key |

### Local Provider URLs

| Variable | Default | Description |
|---|---|---|
| `LM_STUDIO_BASE_URL` | `http://localhost:1234/v1` | LM Studio base URL |
| `LLAMACPP_BASE_URL` | `http://localhost:8080/v1` | llama.cpp server base URL |
| `OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama base URL |

### Proxy Settings

Optional HTTP/SOCKS5 proxy per provider:

| Variable | Format |
|---|---|
| `NVIDIA_NIM_PROXY` | `http://user:pass@host:port` or `socks5://host:port` |
| `OPENROUTER_PROXY` | Same as above |
| `LMSTUDIO_PROXY` | Same as above |
| `LLAMACPP_PROXY` | Same as above |
| `OLLAMA_PROXY` | Same as above |

### Thinking Output

| Variable | Default | Description |
|---|---|---|
| `ENABLE_MODEL_THINKING` | `true` | Global default for extended thinking output |
| `ENABLE_OPUS_THINKING` | _(inherit)_ | Per-model thinking override (`true`/`false`) |
| `ENABLE_SONNET_THINKING` | _(inherit)_ | Per-model thinking override (`true`/`false`) |
| `ENABLE_HAIKU_THINKING` | _(inherit)_ | Per-model thinking override (`true`/`false`) |

### Authentication

| Variable | Default | Description |
|---|---|---|
| `ANTHROPIC_AUTH_TOKEN` | _(empty = open)_ | Token clients must send; leave empty to allow all |

### Rate Limiting

| Variable | Default | Description |
|---|---|---|
| `RATE_LIMIT_RPM` | `60` | Max requests per minute (sliding window) |
| `RATE_LIMIT_RPD` | `1000` | Max requests per day (sliding window) |
| `CONCURRENCY_LIMIT` | `5` | Max simultaneous in-flight provider requests |

### HTTP Timeouts (seconds)

| Variable | Default | Description |
|---|---|---|
| `HTTP_READ_TIMEOUT` | `300` | Max seconds to wait for a full provider response |
| `HTTP_WRITE_TIMEOUT` | `10` | Max seconds to write the outbound request body |
| `HTTP_CONNECT_TIMEOUT` | `10` | Dial + TLS handshake timeout in seconds |
| `HTTP_RESPONSE_HEADER_TIMEOUT` | `120` | Max wait for first response header byte |

### Diagnostics

| Variable | Default | Description |
|---|---|---|
| `LOG_RAW_API_PAYLOADS` | `false` | Log full outbound/inbound payloads (sensitive!) |
| `LOG_RAW_SSE_EVENTS` | `false` | Log each raw SSE line from provider (sensitive!) |

### Discord Bot

| Variable | Description |
|---|---|
| `DISCORD_BOT_TOKEN` | Discord bot token (optional) |
| `DISCORD_CHANNEL_ID` | Discord channel ID the bot listens to (optional) |

### Telegram Bot

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token (optional) |
| `TELEGRAM_CHAT_ID` | Telegram chat ID the bot responds to (optional) |

### Web Tools

| Variable | Default | Description |
|---|---|---|
| `ENABLE_WEB_SERVER_TOOLS` | `false` | Let the LLM call `web_fetch`/`web_search` tools |
| `WEB_FETCH_ALLOWED_SCHEMES` | `http,https` | Comma-separated URL schemes allowed for web tools |
| `WEB_FETCH_ALLOW_PRIVATE_NETWORKS` | `false` | Allow fetching private/internal IPs (off in production) |

### Voice Transcription

| Variable | Default | Description |
|---|---|---|
| `VOICE_NOTE_ENABLED` | `false` | Transcribe voice messages in Discord/Telegram bots |
| `WHISPER_DEVICE` | `cpu` | Transcription backend: `cpu`, `cuda`, or `nvidia_nim` |
| `WHISPER_MODEL` | `base` | Whisper model size: `tiny`, `base`, `small`, `medium`, `large` |
| `HF_TOKEN` | — | Hugging Face token (required by some Whisper models) |

### Session Management

| Variable | Default | Description |
|---|---|---|
| `MESSAGING_SESSION_TIMEOUT` | `300` | Bot session idle timeout in seconds |
| `SMOKE_TEST_ON_STARTUP` | `false` | Send a test request to each provider on startup |

---

## API Usage

The proxy provides OpenAI-compatible endpoints. Use the `x-api-key` or `Authorization` header for authentication.

### Chat Completions

```bash
curl http://localhost:8082/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "x-api-key: automell" \
  -d '{
    "model": "default",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "What is the capital of France?"}
    ],
    "stream": false
  }'
```

### Streaming

```bash
curl http://localhost:8082/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "x-api-key: automell" \
  -d '{
    "model": "default",
    "messages": [{"role": "user", "content": "Tell me a story"}],
    "stream": true
  }'
```

### Model Selection

Use `model` parameter to select a specific model:

```bash
curl http://localhost:8082/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "x-api-key: automell" \
  -d '{
    "model": "opus",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Valid model names: `default`, `opus`, `sonnet`, `haiku`

---

## Messaging Bots

When either Discord or Telegram credentials are configured, `automell bot` starts a bot that relays messages through the proxy.

**Discord** — connects via the Discord Gateway (WebSocket), listens for `MESSAGE_CREATE` events in the configured channel, and replies using the configured model.

**Telegram** — uses long polling, restricts responses to the configured `TELEGRAM_CHAT_ID`.

Both bots call the local proxy, so they benefit from all the same routing, rate limiting, and provider configuration.

### Bot Commands

| Command | Description |
|---------|-------------|
| `/stop` | Cancel the current in-progress request (or all requests in the chat) |
| `/clear` | Cancel all requests and clear session history |
| `/stats` | Show session statistics (total requests, active sessions) |

Reply to a specific bot message with `/stop` or `/clear` to scope the action to that reply branch.

### Web Tools

Set `ENABLE_WEB_SERVER_TOOLS=true` to allow the LLM to call two built-in tools:
- `web_fetch` — fetch a URL and return the content (up to 512 KB)
- `web_search` — search the web via DuckDuckGo instant answers

SSRF protection is enabled by default — private/loopback addresses are blocked.

### Voice Transcription

Set `VOICE_NOTE_ENABLED=true` to transcribe voice messages:
- **Local (default):** requires `openai-whisper` installed (`pip install openai-whisper`)
- **NVIDIA NIM:** set `WHISPER_DEVICE=nvidia_nim` (requires `NVIDIA_NIM_API_KEY`)

```bash
# .env
DISCORD_BOT_TOKEN=your-discord-bot-token
DISCORD_CHANNEL_ID=123456789012345678

# or for Telegram
TELEGRAM_BOT_TOKEN=123456:ABCdef...
TELEGRAM_CHAT_ID=-100123456789
```

```bash
automell bot
```

> **Note:** The bot connects to the local proxy. Run `automell serve` in a separate terminal before starting the bot.

---

## Configuration Examples

### OpenRouter Only

```env
OPENROUTER_API_KEY=your_key
MODEL_OPUS=open_router/anthropic/claude-opus-4-5
MODEL_SONNET=open_router/openai/gpt-4o
MODEL_HAIKU=open_router/meta-llama/llama-3.1-8b-instruct
MODEL=open_router/anthropic/claude-3.5-sonnet
```

### NVIDIA NIM Only

```env
NVIDIA_NIM_API_KEY=your_key
MODEL_OPUS=nvidia_nim/meta/llama-3.1-405b-instruct
MODEL_SONNET=nvidia_nim/meta/llama-3.1-70b-instruct
MODEL=nvidia_nim/meta/llama-3.1-70b-instruct
```

### Fully Local (LM Studio)

```env
LM_STUDIO_BASE_URL=http://localhost:1234/v1
MODEL_OPUS=lmstudio/local-model
MODEL_SONNET=lmstudio/local-model
MODEL_HAIKU=lmstudio/local-model
MODEL=lmstudio/local-model
```

### Ollama

```env
OLLAMA_BASE_URL=http://localhost:11434
MODEL=ollama/llama3.2
```

### Mixed Providers

```env
NVIDIA_NIM_API_KEY=your_nim_key
OPENROUTER_API_KEY=your_openrouter_key
DEEPSEEK_API_KEY=your_deepseek_key

MODEL_OPUS=nvidia_nim/meta/llama-3.1-405b-instruct
MODEL_SONNET=open_router/anthropic/claude-3.5-sonnet
MODEL_HAIKU=deepseek/deepseek-chat
MODEL=nvidia_nim/meta/llama-3.1-70b-instruct
```

---

## Cross-Compiling

```bash
# Linux (amd64)
GOOS=linux GOARCH=amd64 go build -o automell-linux-amd64 .

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o automell-darwin-arm64 .

# Windows (amd64)
GOOS=windows GOARCH=amd64 go build -o automell-windows-amd64.exe .
```

---

## Troubleshooting

### Server won't start

- Check if the port is already in use: `netstat -an | grep 8082` (Linux/macOS) or `netstat -an | findstr 8082` (Windows)
- Verify your `.env` file exists and is properly formatted
- Check logs for specific error messages

### Provider connection errors

- Verify your API keys are correct
- Check if the provider service is operational
- For local providers (LM Studio, Ollama), ensure they are running
- Increase `HTTP_RESPONSE_HEADER_TIMEOUT` for slow providers

### Rate limit errors

- Adjust `RATE_LIMIT_RPM`, `RATE_LIMIT_RPD`, or `CONCURRENCY_LIMIT` in `.env`
- Check if you're hitting provider-side rate limits

### Bot not responding

- Verify bot token and channel/chat ID are correct
- Ensure the bot has permissions in the channel
- Check that the proxy server is running (bot depends on it)
- Verify `MODEL` is configured correctly

### Voice transcription not working

- Ensure `VOICE_NOTE_ENABLED=true`
- Check that Whisper dependencies are installed
- For CUDA, verify GPU drivers are properly installed
- Check logs for transcription errors

### Claude Code connection issues

- Verify `ANTHROPIC_BASE_URL` is set correctly
- Check that `ANTHROPIC_AUTH_TOKEN` matches the value in `.env` (or is any non-empty string if `.env` has empty `ANTHROPIC_AUTH_TOKEN`)
- Ensure the proxy server is running

---

## Repository Layout

```
automell/
├── main.go                  # Entry point: config, HTTP server, graceful shutdown
├── go.mod                   # Module: github.com/dhawalhost/automell
├── .env.example             # All environment variables documented with examples
│
├── config/config.go         # Config struct + Load() — reads .env then env vars
├── dotenv/dotenv.go         # Zero-dependency .env file parser
├── types/types.go           # All wire types: Anthropic + OpenAI request/response/SSE
├── providers/providers.go   # Resolve("nvidia_nim/model") → Provider{BaseURL, APIKey}
├── ratelimit/limiter.go     # SlidingWindowLimiter + ConcurrencyLimiter
│
├── proxy/
│   ├── server.go            # ServeHTTP: auth, rate-limit, /v1/models, orchestration
│   ├── translate.go         # Anthropic ↔ OpenAI request/response translation
│   ├── stream.go            # SSE state machine: OpenAI stream → Anthropic SSE events
│   ├── optimize.go          # Local interceptors for trivial Claude Code requests
│   ├── toolparser.go        # Heuristic tool-call extractor (JSON/XML/function syntax)
│   ├── retry.go             # Exponential backoff for 429 / 5xx provider errors
│   ├── subagent.go          # Task tool interceptor: forces run_in_background=false
│   └── llmclient.go         # HTTP client bots use to call the local proxy
│
├── picker/picker.go         # Interactive TUI model picker (no external UI deps)
├── messaging/
│   ├── discord.go           # Discord bot (gorilla/websocket — raw Gateway API)
│   ├── telegram.go          # Telegram bot (go-telegram-bot-api — long polling)
│   ├── session.go          # Session management for bots
│   └── voice.go             # Whisper transcription wrapper
└── cli/cli.go               # Subcommands: serve / pick / bot
```

---

## Extending

**Add a provider** — open `providers/providers.go` and add a new `case` to the `Resolve` switch with the provider's base URL and API key lookup.

**Add a local optimisation** — open `proxy/optimize.go`, write a detection function (e.g. `isMyPattern`), and return a canned `AResponse` from `tryOptimize`.

**Add a bot platform** — implement the `messaging.LLMCaller` interface (`Chat(string) (string, error)`) and pass a `proxy.NewLLMClient` instance to your bot constructor.

---

## License

Apache 2.0
