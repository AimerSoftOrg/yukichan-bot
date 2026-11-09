package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type responseBodyFunc func([]byte) (int, error)

func (f responseBodyFunc) Read(b []byte) (int, error) { return f(b) }
func (f responseBodyFunc) Close() error               { return nil }

func TestCancellationWhileReadingResponse(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "canceled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			want := error(context.Canceled)
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			readStarted := false
			hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: responseBodyFunc(func([]byte) (int, error) {
					readStarted = true
					if !deadline {
						cancel()
					}
					<-ctx.Done()
					return 0, ctx.Err()
				})}, nil
			})}
			c, err := New(ProviderConfig{Provider: "deepseek", Model: "m", APIKey: "key"}, hc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Generate(ctx, Request{Messages: []Message{{RoleUser, "hello"}}})
			if !readStarted || !errors.Is(err, want) {
				t.Fatalf("read started = %v, got %v, want %v", readStarted, err, want)
			}
		})
	}
}

func TestProviders(t *testing.T) {
	for _, tc := range []struct{ kind, path, response string }{
		{"deepseek", "/chat/completions", `{"model":"actual","choices":[{"message":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`},
		{"chatgpt", "/responses", `{"model":"actual","status":"completed","output":[{"type":"reasoning","content":[{"type":"output_text","text":"hidden"}]},{"type":"message","content":[{"type":"output_text","text":"hel"},{"type":"output_text","text":"lo"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`},
		{"gemini", "/models/override:generateContent", `{"modelVersion":"actual","candidates":[{"content":{"parts":[{"text":"hidden","thought":true},{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing content type")
				}
				if tc.kind == "gemini" {
					if r.Header.Get("x-goog-api-key") != "secret" || r.URL.RawQuery != "" {
						t.Error("incorrect Gemini authentication")
					}
				} else if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing bearer token")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if tc.kind == "gemini" {
					contents := body["contents"].([]any)
					if len(contents) != 3 || contents[1].(map[string]any)["role"] != "model" {
						t.Error("incorrect history mapping")
					}
					if len(body["systemInstruction"].(map[string]any)["parts"].([]any)) != 2 {
						t.Error("system messages lost")
					}
					gc := body["generationConfig"].(map[string]any)
					if gc["temperature"] != float64(0) || gc["maxOutputTokens"] != float64(10) {
						t.Error("generation config lost")
					}
				} else {
					field, tokens := "messages", "max_tokens"
					if tc.kind == "chatgpt" {
						field, tokens = "input", "max_output_tokens"
						if body["store"] != false {
							t.Error("storage not disabled")
						}
					}
					if len(body[field].([]any)) != 5 || body["model"] != "override" || body["temperature"] != float64(0) || body[tokens] != float64(10) {
						t.Errorf("incorrect request: %v", body)
					}
				}
				w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			c, err := New(ProviderConfig{Provider: tc.kind, Model: "default", APIKey: "secret", BaseURL: srv.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			temp := 0.0
			res, err := c.Generate(context.Background(), Request{Model: "override", Temperature: &temp, MaxOutputTokens: 10, Messages: []Message{{RoleSystem, "system"}, {RoleSystem, "more"}, {RoleUser, "question"}, {RoleAssistant, "answer"}, {RoleUser, "followup"}}})
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != "hello" || res.Model != "actual" || res.FinishReason == "" || res.Usage != (Usage{3, 2, 5}) {
				t.Fatalf("unexpected response: %+v", res)
			}
		})
	}
}

func TestErrorsAndCancellation(t *testing.T) {
	for _, kind := range []string{"chatgpt", "deepseek", "gemini"} {
		for _, tc := range []struct {
			name   string
			status int
			body   string
			target error
		}{
			{"rate limit", 429, "secret prompt", nil}, {"malformed", 200, "invalid", nil}, {"empty", 200, "{}", ErrEmptyResponse},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
				defer srv.Close()
				c, _ := New(ProviderConfig{Provider: kind, Model: "m", APIKey: "secret", BaseURL: srv.URL}, nil)
				_, err := c.Generate(context.Background(), Request{Messages: []Message{{RoleUser, "hi"}}})
				if err == nil || strings.Contains(err.Error(), "secret") {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.target != nil && !errors.Is(err, tc.target) {
					t.Fatalf("got %v", err)
				}
				if tc.status == 429 {
					var apiErr *APIError
					if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
						t.Fatalf("got %v", err)
					}
				}
			})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	c, _ := New(ProviderConfig{Provider: "deepseek", Model: "m", APIKey: "key", BaseURL: srv.URL}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Generate(ctx, Request{Messages: []Message{{RoleUser, "hi"}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestConfigurationAndRegistry(t *testing.T) {
	defer Configure(Config{})
	t.Setenv("LLM_TEST_KEY", "from-env")
	cfg := ProviderConfig{Provider: "deepseek", Model: "model", APIKeyEnv: "LLM_TEST_KEY"}
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := c.(*provider)
	if p.key != "from-env" || p.http.Timeout != time.Minute {
		t.Fatal("incorrect defaults")
	}
	for _, mutate := range []func(*ProviderConfig){
		func(c *ProviderConfig) { c.Provider = "unknown" }, func(c *ProviderConfig) { c.Model = "" }, func(c *ProviderConfig) { c.APIKeyEnv = "MISSING_LLM_TEST_ENV" }, func(c *ProviderConfig) { c.BaseURL = "file:///tmp" }, func(c *ProviderConfig) { c.TimeoutSeconds = -1 },
	} {
		copy := cfg
		mutate(&copy)
		if _, err := New(copy, nil); err == nil {
			t.Fatalf("accepted invalid config: %+v", copy)
		}
	}
	if err := Configure(Config{Default: "primary", Providers: map[string]ProviderConfig{"primary": cfg}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("primary"); err != nil {
		t.Fatal(err)
	}
	if err := Configure(Config{Default: "missing"}); err == nil {
		t.Fatal("accepted invalid default")
	}
	if _, err := Get("primary"); err != nil {
		t.Fatal("invalid config replaced registry")
	}
	if err := Configure(Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestRequestValidation(t *testing.T) {
	c, _ := New(ProviderConfig{Provider: "deepseek", Model: "m", APIKey: "key"}, nil)
	for _, req := range []Request{{}, {Messages: []Message{{"bad", "hello"}}}, {Messages: []Message{{RoleUser, " "}}}, {Messages: []Message{{RoleUser, "hi"}}, MaxOutputTokens: -1}} {
		if _, err := c.Generate(context.Background(), req); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
}

func TestDefaultsAndDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["model"] != "default" {
			t.Error("default model lost")
		}
		if _, ok := body["temperature"]; ok {
			t.Error("nil temperature must be omitted")
		}
		if _, ok := body["max_tokens"]; ok {
			t.Error("default token limit must be omitted")
		}
		time.Sleep(40 * time.Millisecond)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	c, err := New(ProviderConfig{Provider: "deepseek", Model: "default", APIKey: "secret", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := c.Generate(ctx, Request{Messages: []Message{{RoleUser, "hello"}}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}
