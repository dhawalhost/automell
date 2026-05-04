package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dhawalhost/automell/config"
	"github.com/dhawalhost/automell/providers"
	"github.com/dhawalhost/automell/ratelimit"
	"github.com/dhawalhost/automell/types"
)

// Server represents the proxy server
type Server struct {
	config             *config.Config
	rateLimiter        *ratelimit.SlidingWindowLimiter
	dailyLimiter       *ratelimit.SlidingWindowLimiter
	concurrencyLimiter *ratelimit.ConcurrencyLimiter
	defaultClient      *http.Client
	// proxyClients caches per-proxy-URL http.Client instances (created lazily).
	proxyClients   map[string]*http.Client
	proxyClientsMu sync.Mutex
	// shutdownCtx is cancelled when Shutdown is called, immediately aborting
	// all in-flight streaming provider requests so the HTTP server can drain fast.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

type countingReadCloser struct {
	rc    io.ReadCloser
	bytes atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 {
		c.bytes.Add(int64(n))
	}
	return n, err
}

func (c *countingReadCloser) Close() error {
	return c.rc.Close()
}

func (c *countingReadCloser) BytesRead() int64 {
	return c.bytes.Load()
}

// lockedStreamWriter serializes writes/flushes from translator and keepalive ticker.
type lockedStreamWriter struct {
	w  http.ResponseWriter
	mu sync.Mutex
}

func newLockedStreamWriter(w http.ResponseWriter) *lockedStreamWriter {
	return &lockedStreamWriter{w: w}
}

func (lw *lockedStreamWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

func (lw *lockedStreamWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if f, ok := lw.w.(http.Flusher); ok {
		f.Flush()
	}
}

func (lw *lockedStreamWriter) WriteKeepAliveComment() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	fmt.Fprintf(lw.w, ": keep-alive %d\n\n", time.Now().Unix())
	if f, ok := lw.w.(http.Flusher); ok {
		f.Flush()
	}
}

var requestSeq atomic.Uint64

func nextRequestID() string {
	return fmt.Sprintf("req-%d", requestSeq.Add(1))
}

func appendUniqueModel(dst []string, seen map[string]bool, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" || seen[model] {
		return dst
	}
	seen[model] = true
	return append(dst, model)
}

func candidateProviderModels(cfg *config.Config, claudeModel string) []string {
	seen := make(map[string]bool)
	var models []string

	// Primary route first.
	models = appendUniqueModel(models, seen, cfg.ResolveModel(claudeModel))

	// Simple fixed fallback order:
	// 1) MODEL (global fallback)
	// 2) MODEL_SONNET
	// 3) MODEL_HAIKU
	// 4) MODEL_OPUS
	models = appendUniqueModel(models, seen, cfg.Model)
	models = appendUniqueModel(models, seen, cfg.ModelSonnet)
	models = appendUniqueModel(models, seen, cfg.ModelHaiku)
	models = appendUniqueModel(models, seen, cfg.ModelOpus)
	return models
}

func isModelNotFound(statusCode int, body string) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	l := strings.ToLower(body)
	return strings.Contains(l, "model not found") || strings.Contains(l, "invalid_request_error")
}

func (lrw *loggingResponseWriter) WriteHeader(statusCode int) {
	lrw.statusCode = statusCode
	lrw.ResponseWriter.WriteHeader(statusCode)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.statusCode == 0 {
		lrw.statusCode = http.StatusOK
	}
	return lrw.ResponseWriter.Write(b)
}

func requestIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}

	return r.RemoteAddr
}

// NewServer creates a new proxy server
func NewServer(cfg *config.Config) *Server {
	rpmLimiter := ratelimit.NewSlidingWindowLimiter(cfg.RateLimitRPM, time.Minute)
	rpdLimiter := ratelimit.NewSlidingWindowLimiter(cfg.RateLimitRPD, 24*time.Hour)
	concLimiter := ratelimit.NewConcurrencyLimiter(cfg.ConcurrencyLimit)

	connectTimeout := time.Duration(cfg.HTTPConnectTimeoutS) * time.Second
	respHeaderTimeout := time.Duration(cfg.HTTPResponseHeaderTimeoutS) * time.Second

	defaultClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			// Max wait for the provider to send the first response header byte.
			// Set via HTTP_RESPONSE_HEADER_TIMEOUT (default 120s). Must be generous
			// enough for slow providers (NIM cold starts) but finite to catch hangs.
			ResponseHeaderTimeout: respHeaderTimeout,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &Server{
		config:             cfg,
		rateLimiter:        rpmLimiter,
		dailyLimiter:       rpdLimiter,
		concurrencyLimiter: concLimiter,
		defaultClient:      defaultClient,
		proxyClients:       make(map[string]*http.Client),
		shutdownCtx:        shutdownCtx,
		shutdownCancel:     shutdownCancel,
	}
}

// Shutdown cancels all active streaming provider requests so the HTTP server
// can drain connections immediately on SIGINT/SIGTERM.
func (s *Server) Shutdown() {
	s.shutdownCancel()
}

// clientForProvider returns the http.Client to use for a given provider.
// If the provider has a proxy URL configured, a cached per-proxy client is returned.
func (s *Server) clientForProvider(provider types.Provider) *http.Client {
	if provider.Proxy == "" {
		return s.defaultClient
	}
	s.proxyClientsMu.Lock()
	defer s.proxyClientsMu.Unlock()
	if c, ok := s.proxyClients[provider.Proxy]; ok {
		return c
	}
	proxyURL, err := url.Parse(provider.Proxy)
	if err != nil {
		log.Printf("Invalid proxy URL %q for provider %s: %v — using default client", provider.Proxy, provider.Name, err)
		return s.defaultClient
	}
	connectTimeout := time.Duration(s.config.HTTPConnectTimeoutS) * time.Second
	respHeaderTimeout := time.Duration(s.config.HTTPResponseHeaderTimeoutS) * time.Second
	c := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: respHeaderTimeout,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	s.proxyClients[provider.Proxy] = c
	return c
}

// ServeHTTP handles incoming HTTP requests
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lrw := &loggingResponseWriter{ResponseWriter: w}
	start := time.Now()
	reqID := nextRequestID()

	defer func() {
		statusCode := lrw.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		log.Printf("[%s] %s %s from=%s status=%d duration=%s", reqID, r.Method, r.URL.Path, requestIP(r), statusCode, time.Since(start).Truncate(time.Millisecond))
	}()

	w = lrw

	// Handle /v1/models
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		s.handleModels(w, r)
		return
	}

	// Handle /v1/messages/count_tokens
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/count_tokens") {
		s.handleCountTokens(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.authenticate(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	log.Printf("[%s] request accepted model=%s stream=%t", reqID, req.Model, req.Stream)

	// Try local optimisation
	if tryOptimize(w, &req) {
		return
	}

	// Rate limiting — context-aware so a disconnected client releases its slot immediately
	if err := s.rateLimiter.WaitContext(r.Context()); err != nil {
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
		return
	}
	if err := s.dailyLimiter.WaitContext(r.Context()); err != nil {
		http.Error(w, "Request cancelled", http.StatusRequestTimeout)
		return
	}
	s.concurrencyLimiter.Acquire()
	defer s.concurrencyLimiter.Release()

	providerModels := candidateProviderModels(s.config, req.Model)
	if len(providerModels) == 0 {
		http.Error(w, "No provider models configured", http.StatusBadRequest)
		return
	}

	for i, providerModel := range providerModels {
		provider, err := providers.Resolve(providerModel)
		if err != nil {
			log.Printf("[%s] skipping model candidate=%s resolve_error=%v", reqID, providerModel, err)
			continue
		}
		log.Printf("[%s] routing attempt=%d/%d provider=%s provider_model=%s", reqID, i+1, len(providerModels), provider.Name, providerModel)

		// Translate request — strip the provider prefix to get the bare model name
		// e.g. "nvidia_nim/meta/llama-3.1-70b-instruct" → "meta/llama-3.1-70b-instruct"
		modelParts := strings.SplitN(providerModel, "/", 2)
		modelName := providerModel
		if len(modelParts) == 2 {
			modelName = modelParts[1]
		}
		oaiReq, err := translateRequest(&req, modelName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to translate request: %v", err), http.StatusBadRequest)
			return
		}

		providerReqBody, err := json.Marshal(oaiReq)
		if err != nil {
			http.Error(w, "Failed to marshal provider request", http.StatusInternalServerError)
			return
		}

		if s.config.LogRawAPIPayloads {
			log.Printf("[DEBUG] outbound payload to %s: %s", provider.Name, string(providerReqBody))
		}

		// For streaming requests we must NOT impose the HTTPReadTimeoutS deadline on the
		// entire response body: generation can legitimately take many minutes and the
		// timeout would kill the stream (exactly the "context deadline exceeded" at 5 m0s
		// symptom).  The connection/dial timeout is already enforced by DialContext on the
		// transport.  Client disconnect or server shutdown will cancel r.Context().
		//
		// For non-streaming we do apply HTTPReadTimeoutS so a hung provider doesn't hold
		// a goroutine indefinitely.
		var reqCtx context.Context
		var cancel context.CancelFunc
		if req.Stream {
			// Derive from shutdownCtx so Ctrl-C/SIGTERM cancels in-flight streams immediately.
			reqCtx, cancel = context.WithCancel(s.shutdownCtx)
			// Also propagate client disconnect: cancel when either fires.
			go func() {
				select {
				case <-r.Context().Done():
					cancel()
				case <-reqCtx.Done():
				}
			}()
		} else {
			timeout := s.config.HTTPReadTimeoutS
			if timeout <= 0 {
				timeout = 300
			}
			reqCtx, cancel = context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
		}

		providerHTTPReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, provider.BaseURL,
			strings.NewReader(string(providerReqBody)))
		if err != nil {
			cancel()
			http.Error(w, "Failed to create provider request", http.StatusInternalServerError)
			return
		}
		providerHTTPReq.Header.Set("Content-Type", "application/json")
		if provider.APIKey != "" {
			providerHTTPReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
		for k, v := range provider.ExtraHeaders {
			providerHTTPReq.Header.Set(k, v)
		}

		httpClient := s.clientForProvider(provider)

		allowFallback := i < len(providerModels)-1
		var fallback bool
		if req.Stream {
			fallback = s.forwardStream(reqID, httpClient, providerHTTPReq, w, req.Model, provider.Name, allowFallback)
		} else {
			fallback = s.forwardNonStream(reqID, httpClient, providerHTTPReq, w, &req, provider, modelName, allowFallback)
		}
		cancel()

		if fallback {
			log.Printf("[%s] fallback triggered after model-not-found on %s", reqID, providerModel)
			continue
		}
		return
	}

	http.Error(w, "All configured model routes failed to resolve or respond", http.StatusBadGateway)
}

func (s *Server) authenticate(r *http.Request) bool {
	if s.config.AnthropicAuthToken == "" {
		return true
	}
	token := r.Header.Get("x-api-key")
	if token == "" {
		token = r.Header.Get("anthropic-auth-token")
	}
	if token == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	// Strip :model override suffix
	if idx := strings.Index(token, ":"); idx != -1 {
		token = token[:idx]
	}
	return token == s.config.AnthropicAuthToken
}

func (s *Server) forwardStream(reqID string, client *http.Client, req *http.Request, w http.ResponseWriter, model string, providerName string, allowFallback bool) bool {
	start := time.Now()
	log.Printf("[%s] stream request started provider=%s model=%s", reqID, providerName, model)

	resp, err := doWithRetry(req.Context(), client, req)
	if err != nil {
		log.Printf("[%s] stream upstream request failed: %v", reqID, err)
		http.Error(w, fmt.Sprintf("Provider request failed: %v", err), http.StatusBadGateway)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[%s] stream upstream non-200 provider=%s status=%d body=%s", reqID, providerName, resp.StatusCode, string(body))
		if allowFallback && isModelNotFound(resp.StatusCode, string(body)) {
			return true
		}
		http.Error(w, fmt.Sprintf("Provider error: %s", string(body)), resp.StatusCode)
		return false
	}
	log.Printf("[%s] stream upstream connected provider=%s status=%d", reqID, providerName, resp.StatusCode)

	bodyCounter := &countingReadCloser{rc: resp.Body}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	log.Printf("[%s] stream response headers sent to client", reqID)

	streamWriter := newLockedStreamWriter(w)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				streamWriter.WriteKeepAliveComment()
				log.Printf("[%s] stream alive provider=%s elapsed=%s upstream_bytes=%d", reqID, providerName, time.Since(start).Truncate(time.Second), bodyCounter.BytesRead())
			case <-done:
				return
			}
		}
	}()

	translator := NewStreamTranslator(streamWriter, s.config.LogRawSSEEvents)
	defer translator.finalize()

	err = translator.Run(bodyCounter, model)
	close(done)

	if err != nil {
		log.Printf("[%s] stream translation error provider=%s elapsed=%s upstream_bytes=%d err=%v", reqID, providerName, time.Since(start).Truncate(time.Millisecond), bodyCounter.BytesRead(), err)
		return false
	}

	log.Printf("[%s] stream completed provider=%s elapsed=%s upstream_bytes=%d", reqID, providerName, time.Since(start).Truncate(time.Millisecond), bodyCounter.BytesRead())
	return false
}

func (s *Server) forwardNonStream(reqID string, client *http.Client, req *http.Request, w http.ResponseWriter, originalReq *types.AnthropicRequest, provider types.Provider, modelName string, allowFallback bool) bool {
	providerName := provider.Name
	start := time.Now()
	log.Printf("[%s] non-stream request started provider=%s", reqID, providerName)

	resp, err := doWithRetry(req.Context(), client, req)
	if err != nil {
		log.Printf("[%s] non-stream upstream request failed: %v", reqID, err)
		http.Error(w, fmt.Sprintf("Provider request failed: %v", err), http.StatusBadGateway)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[%s] non-stream upstream non-200 provider=%s status=%d body=%s", reqID, providerName, resp.StatusCode, string(body))
		if allowFallback && isModelNotFound(resp.StatusCode, string(body)) {
			return true
		}
		http.Error(w, fmt.Sprintf("Provider error: %s", string(body)), resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read provider response", http.StatusInternalServerError)
		return false
	}

	if s.config.LogRawAPIPayloads {
		log.Printf("[DEBUG] provider response: %s", string(body))
	}

	var oaiResp types.OAIResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		http.Error(w, "Failed to parse provider response", http.StatusInternalServerError)
		return false
	}

	anthropicResp, err := translateNonStreamResponse(&oaiResp)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to translate response: %v", err), http.StatusInternalServerError)
		return false
	}

	// Web server tools: if the provider returned web tool calls and the feature is
	// enabled, execute them locally and re-call the provider until we get a text
	// response (up to webToolMaxIterations times).
	if s.config.EnableWebServerTools && len(extractWebToolCalls(anthropicResp)) > 0 {
		log.Printf("[%s] web-tools detected in response, entering tool loop", reqID)
		final, err := runWebToolLoop(req.Context(), s.config, client,
			provider.BaseURL, provider.APIKey, provider.ExtraHeaders,
			originalReq, anthropicResp, modelName, reqID)
		if err != nil {
			log.Printf("[%s] web-tools loop error: %v", reqID, err)
			// Fall through: return what we have
		} else {
			anthropicResp = final
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicResp)
	log.Printf("[%s] non-stream completed provider=%s elapsed=%s", reqID, providerName, time.Since(start).Truncate(time.Millisecond))
	return false
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Content interface{} `json:"content"`
		} `json:"messages"`
		System interface{} `json:"system"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Heuristic: characters / 4
	text := extractTextFromContent(req.System)
	for _, msg := range req.Messages {
		text += extractTextFromContent(msg.Content)
	}
	tokenCount := len(text) / 4

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.CountTokensResponse{InputTokens: tokenCount})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	configured := providers.ConfiguredProviders(s.config)
	providerSet := make(map[string]bool, len(configured))
	for _, p := range configured {
		providerSet[p] = true
	}

	// Claude gateway model IDs always listed (they route to whatever provider is configured)
	modelIDs := []string{
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
	}

	// If any provider capable of thinking is configured, also surface "no-thinking" variants
	// so users can easily pick them in the Claude Code model picker.
	if len(configured) > 0 {
		modelIDs = append(modelIDs,
			"claude-opus-4-5-no-thinking",
			"claude-sonnet-4-5-no-thinking",
			"claude-haiku-4-5-no-thinking",
		)
	}

	models := make([]types.ModelObject, 0, len(modelIDs))
	for _, id := range modelIDs {
		models = append(models, types.ModelObject{ID: id, Object: "model", Created: now, OwnedBy: "anthropic"})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.ModelsResponse{Object: "list", Data: models})
}

// ValidateConfig performs startup checks and logs warnings for any misconfigured
// model routes. It returns an error only when NO provider is reachable at all.
func ValidateConfig(cfg *config.Config) error {
	configured := providers.ConfiguredProviders(cfg)
	if len(configured) == 0 {
		return fmt.Errorf("no providers configured — set at least one of NVIDIA_NIM_API_KEY, OPENROUTER_API_KEY, DEEPSEEK_API_KEY, LM_STUDIO_BASE_URL, LLAMACPP_BASE_URL, or OLLAMA_BASE_URL")
	}

	// Validate the four model routes
	routes := map[string]string{
		"MODEL_OPUS":   cfg.ModelOpus,
		"MODEL_SONNET": cfg.ModelSonnet,
		"MODEL_HAIKU":  cfg.ModelHaiku,
		"MODEL":        cfg.Model,
	}
	for key, model := range routes {
		if model == "" {
			continue
		}
		_, err := providers.Resolve(model)
		if err != nil {
			log.Printf("[WARN] %s=%q — provider resolution failed: %v", key, model, err)
		}
	}

	log.Printf("Configured providers: %s", strings.Join(configured, ", "))

	if cfg.SmokeTestOnStartup {
		runSmokeTests(cfg)
	}
	return nil
}

// runSmokeTests sends a minimal request to each configured provider and logs PASS/FAIL.
// Failures are warnings only — they do not block startup.
func runSmokeTests(cfg *config.Config) {
	configured := providers.ConfiguredProviders(cfg)
	if len(configured) == 0 {
		return
	}
	log.Printf("[smoke] running startup smoke tests for %d provider(s)...", len(configured))

	connectTimeout := time.Duration(cfg.HTTPConnectTimeoutS) * time.Second
	smokeClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectTimeout,
			}).DialContext,
		},
	}

	type smokeReq struct {
		Model     string        `json:"model"`
		Messages  []interface{} `json:"messages"`
		MaxTokens int           `json:"max_tokens"`
		Stream    bool          `json:"stream"`
	}

	for _, providerModel := range configured {
		provider, err := providers.Resolve(providerModel)
		if err != nil {
			log.Printf("[smoke] SKIP provider=%s reason=%v", providerModel, err)
			continue
		}
		modelParts := strings.SplitN(providerModel, "/", 2)
		modelName := providerModel
		if len(modelParts) == 2 {
			modelName = modelParts[1]
		}
		body, _ := json.Marshal(smokeReq{
			Model: modelName,
			Messages: []interface{}{
				map[string]string{"role": "user", "content": "hi"},
			},
			MaxTokens: 1,
			Stream:    false,
		})
		req, err := http.NewRequest(http.MethodPost, provider.BaseURL, strings.NewReader(string(body)))
		if err != nil {
			log.Printf("[smoke] FAIL provider=%s error=%v", provider.Name, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if provider.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
		for k, v := range provider.ExtraHeaders {
			req.Header.Set(k, v)
		}
		resp, err := smokeClient.Do(req)
		if err != nil {
			log.Printf("[smoke] FAIL provider=%s error=%v", provider.Name, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[smoke] PASS provider=%s status=%d", provider.Name, resp.StatusCode)
		} else {
			log.Printf("[smoke] FAIL provider=%s status=%d", provider.Name, resp.StatusCode)
		}
	}
}

func extractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var out strings.Builder
		for _, item := range v {
			out.WriteString(extractTextFromContent(item))
		}
		return out.String()
	case map[string]interface{}:
		if t, ok := v["type"].(string); ok {
			switch t {
			case "text", "thinking":
				if text, ok := v["text"].(string); ok {
					return text
				}
				if thinking, ok := v["thinking"].(string); ok {
					return thinking
				}
			case "tool_result":
				if c, ok := v["content"]; ok {
					return extractTextFromContent(c)
				}
			}
		}
		if text, ok := v["text"].(string); ok {
			return text
		}
		return ""
	default:
		return ""
	}
}
