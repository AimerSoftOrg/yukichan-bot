package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// APIError omits the response body, which can contain prompts or credentials.
// Callers can use errors.As to inspect StatusCode for rate limits and retries.
type APIError struct {
	Provider   string
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: %s API returned HTTP %d", e.Provider, e.StatusCode)
}

func (p *provider) post(ctx context.Context, endpoint string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("llm: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("llm: cannot build HTTP request")
	}
	req.Header.Set("Content-Type", "application/json")
	if p.kind == "gemini" {
		req.Header.Set("x-goog-api-key", p.key)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	res, err := p.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Avoid returning URL errors that could include sensitive custom endpoints.
		return fmt.Errorf("llm: %s HTTP request failed", p.kind)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &APIError{Provider: p.kind, StatusCode: res.StatusCode}
	}
	const limit = 8 << 20
	data, err = io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("llm: cannot read API response")
	}
	if len(data) > limit {
		return errors.New("llm: API response too large")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return errors.New("llm: invalid JSON response")
	}
	return nil
}
