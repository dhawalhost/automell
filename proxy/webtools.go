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
	"time"

	"github.com/dhawalhost/automell/config"
	"github.com/dhawalhost/automell/types"
)

const (
	webToolMaxBodyBytes  = 512 * 1024 // 512 KB
	webToolFetchTimeout  = 30 * time.Second
	webToolMaxIterations = 3
)

// isWebToolCall returns true for tool names this proxy handles locally.
func isWebToolCall(name string) bool {
	return name == "web_fetch" || name == "web_search"
}

// egressPolicy enforces SSRF and scheme restrictions on a URL.
// It returns nil when the URL is safe to fetch.
func egressPolicy(rawURL string, cfg *config.Config) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	allowed := cfg.WebFetchSchemesSet()
	if !allowed[scheme] {
		return fmt.Errorf("scheme %q is not in WEB_FETCH_ALLOWED_SCHEMES", scheme)
	}

	if !cfg.WebFetchAllowPrivateNetworks {
		host := parsed.Hostname()
		addrs, err := net.LookupHost(host)
		if err != nil {
			return fmt.Errorf("hostname resolution failed for %q: %w", host, err)
		}
		for _, addr := range addrs {
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}
			if isPrivateIP(ip) {
				return fmt.Errorf("URL %q resolves to a private/reserved IP address (%s); set WEB_FETCH_ALLOW_PRIVATE_NETWORKS=true to override", rawURL, addr)
			}
		}
	}
	return nil
}

// isPrivateIP returns true for loopback, private, link-local and other
// reserved IP ranges that should not be reachable from public web tools.
func isPrivateIP(ip net.IP) bool {
	private := []string{
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // private class A
		"172.16.0.0/12",  // private class B
		"192.168.0.0/16", // private class C
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
		"100.64.0.0/10",  // shared address space (CGNAT)
		"192.0.0.0/24",   // IETF protocol assignments
		"198.18.0.0/15",  // benchmarking
		"224.0.0.0/4",    // multicast
		"240.0.0.0/4",    // reserved
	}
	for _, cidr := range private {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// fetchURL performs an HTTP GET and returns the response body as a string.
func fetchURL(ctx context.Context, rawURL string, cfg *config.Config) (string, error) {
	if err := egressPolicy(rawURL, cfg); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, webToolFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "automell/1.0 (web-fetch)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, webToolMaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	return fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body)), nil
}

// searchWeb queries the DuckDuckGo instant answer API and returns results as text.
func searchWeb(ctx context.Context, query string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, webToolFetchTimeout)
	defer cancel()

	ddgURL := "https://api.duckduckgo.com/?q=" + url.QueryEscape(query) + "&format=json&no_html=1&skip_disambig=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ddgURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("User-Agent", "automell/1.0 (web-search)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, webToolMaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading search response: %w", err)
	}

	// Parse DuckDuckGo JSON response and extract readable text
	var ddg struct {
		AbstractText   string `json:"AbstractText"`
		AbstractSource string `json:"AbstractSource"`
		AbstractURL    string `json:"AbstractURL"`
		Answer         string `json:"Answer"`
		Definition     string `json:"Definition"`
		RelatedTopics  []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err := json.Unmarshal(body, &ddg); err != nil {
		// Return raw body if parsing fails
		return string(body), nil
	}

	var sb strings.Builder
	sb.WriteString("Web search results for: " + query + "\n\n")

	if ddg.Answer != "" {
		sb.WriteString("Answer: " + ddg.Answer + "\n\n")
	}
	if ddg.AbstractText != "" {
		sb.WriteString("Summary (" + ddg.AbstractSource + "): " + ddg.AbstractText + "\n")
		if ddg.AbstractURL != "" {
			sb.WriteString("Source: " + ddg.AbstractURL + "\n")
		}
		sb.WriteString("\n")
	}
	if ddg.Definition != "" {
		sb.WriteString("Definition: " + ddg.Definition + "\n\n")
	}
	if len(ddg.RelatedTopics) > 0 {
		sb.WriteString("Related topics:\n")
		limit := 5
		if len(ddg.RelatedTopics) < limit {
			limit = len(ddg.RelatedTopics)
		}
		for _, t := range ddg.RelatedTopics[:limit] {
			if t.Text != "" {
				sb.WriteString("- " + t.Text + "\n")
				if t.FirstURL != "" {
					sb.WriteString("  " + t.FirstURL + "\n")
				}
			}
		}
	}

	result := sb.String()
	if strings.TrimSpace(result) == "Web search results for: "+query {
		result += "(No instant answer found. Try a more specific query.)\n"
	}
	return result, nil
}

// extractWebToolCalls returns tool_use blocks from an AResponse whose names
// are handled locally (web_fetch / web_search).
func extractWebToolCalls(resp *types.AResponse) []types.AContentBlock {
	var out []types.AContentBlock
	for _, blk := range resp.Content {
		if blk.Type == "tool_use" && isWebToolCall(blk.Name) {
			out = append(out, blk)
		}
	}
	return out
}

// executeWebTools runs each web tool call and returns tool_result blocks.
func executeWebTools(ctx context.Context, toolUses []types.AContentBlock, cfg *config.Config) []types.AContentBlock {
	results := make([]types.AContentBlock, 0, len(toolUses))
	for _, tu := range toolUses {
		var inp types.WebToolInput
		if err := json.Unmarshal(tu.Input, &inp); err != nil {
			results = append(results, errorToolResult(tu.ID, "invalid tool input: "+err.Error()))
			continue
		}

		var text string
		var ferr error
		switch tu.Name {
		case "web_fetch":
			if inp.URL == "" {
				text = "(error: web_fetch requires a 'url' parameter)"
			} else {
				text, ferr = fetchURL(ctx, inp.URL, cfg)
			}
		case "web_search":
			if inp.Query == "" {
				text = "(error: web_search requires a 'query' parameter)"
			} else {
				text, ferr = searchWeb(ctx, inp.Query)
			}
		}
		if ferr != nil {
			log.Printf("[web-tools] tool %s failed: %v", tu.Name, ferr)
			results = append(results, errorToolResult(tu.ID, ferr.Error()))
		} else {
			results = append(results, successToolResult(tu.ID, text))
		}
	}
	return results
}

func errorToolResult(toolUseID, errMsg string) types.AContentBlock {
	content, _ := json.Marshal(errMsg)
	return types.AContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}
}

func successToolResult(toolUseID, text string) types.AContentBlock {
	content, _ := json.Marshal(text)
	return types.AContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}
}

// runWebToolLoop executes web tool calls returned by the provider, appends the
// results back to the messages, and calls the provider again (up to maxIterations).
// It returns the final AResponse (which should be a text response, not more tool calls).
func runWebToolLoop(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	providerBaseURL string,
	providerAPIKey string,
	providerExtraHeaders map[string]string,
	originalReq *types.AnthropicRequest,
	firstResp *types.AResponse,
	modelName string,
	reqID string,
) (*types.AResponse, error) {
	currentResp := firstResp
	messages := make([]types.AMessage, len(originalReq.Messages))
	copy(messages, originalReq.Messages)

	for iter := 0; iter < webToolMaxIterations; iter++ {
		webCalls := extractWebToolCalls(currentResp)
		if len(webCalls) == 0 {
			break // done — response contains no more web tool calls
		}
		log.Printf("[%s] web-tools iter=%d executing %d tool call(s)", reqID, iter+1, len(webCalls))

		// Append the assistant's tool_use message
		assistantContent, _ := json.Marshal(currentResp.Content)
		messages = append(messages, types.AMessage{
			Role:    "assistant",
			Content: json.RawMessage(assistantContent),
		})

		// Execute tools and build tool_result message
		results := executeWebTools(ctx, webCalls, cfg)
		toolResultContent, _ := json.Marshal(results)
		messages = append(messages, types.AMessage{
			Role:    "user",
			Content: json.RawMessage(toolResultContent),
		})

		// Build a new request with the updated message history
		nextAnthropicReq := *originalReq
		nextAnthropicReq.Messages = messages
		nextAnthropicReq.Stream = false

		oaiReq, err := translateRequest(&nextAnthropicReq, modelName)
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d translate: %w", iter, err)
		}
		body, err := json.Marshal(oaiReq)
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d marshal: %w", iter, err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, providerBaseURL, strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d new request: %w", iter, err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if providerAPIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+providerAPIKey)
		}
		for k, v := range providerExtraHeaders {
			httpReq.Header.Set(k, v)
		}

		respHTTP, err := httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d provider request: %w", iter, err)
		}
		respBody, err := io.ReadAll(respHTTP.Body)
		respHTTP.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d read body: %w", iter, err)
		}
		if respHTTP.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("web tool loop iter=%d provider status %d: %s", iter, respHTTP.StatusCode, string(respBody))
		}

		var oaiResp types.OAIResponse
		if err := json.Unmarshal(respBody, &oaiResp); err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d parse OAI response: %w", iter, err)
		}
		anthropicResp, err := translateNonStreamResponse(&oaiResp)
		if err != nil {
			return nil, fmt.Errorf("web tool loop iter=%d translate response: %w", iter, err)
		}
		currentResp = anthropicResp
	}

	return currentResp, nil
}
