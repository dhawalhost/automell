package proxy

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

// doWithRetry executes an HTTP request with exponential backoff retry.
// Retries on 429, 502, 503, 504 status codes.
// Honours Retry-After headers from the provider.
// The request must have GetBody set (automatic for strings.NewReader / bytes.NewReader bodies).
func doWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	var lastResp *http.Response
	var lastErr error

	maxRetries := 5
	baseDelay := 1 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Reset the body for every attempt after the first so retries send the
		// full payload rather than an empty reader at EOF.
		if attempt > 0 {
			if req.GetBody == nil {
				return nil, fmt.Errorf("request body is not rewindable; cannot retry")
			}
			newBody, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to reset request body for retry: %w", err)
			}
			req.Body = newBody

			// Calculate delay with exponential backoff + jitter
			delay := baseDelay * time.Duration(1<<(attempt-1))

			// Check for Retry-After header in last response
			if lastResp != nil {
				if retryAfter := lastResp.Header.Get("Retry-After"); retryAfter != "" {
					if seconds, err := strconv.Atoi(retryAfter); err == nil {
						delay = time.Duration(seconds) * time.Second
					}
				}
			}

			// Add up to 25% jitter to spread out concurrent retries
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			delay += jitter

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			// Don't retry on context cancellation or timeouts — they indicate
			// the caller gave up or the server is too slow; retrying just piles on.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return nil, fmt.Errorf("provider timeout: %w", err)
			}
			lastErr = err
			continue
		}

		// Check if we should retry
		if shouldRetry(resp.StatusCode) {
			// Drain body before retry
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			lastResp = resp
			lastErr = fmt.Errorf("provider returned status %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
	}

	return nil, fmt.Errorf("max retries exceeded")
}

func shouldRetry(statusCode int) bool {
	return statusCode == 429 || statusCode == 502 || statusCode == 503 || statusCode == 504
}
