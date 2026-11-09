// Package llm provides stateless text generation shared by bot plugins.
package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}
type Request struct {
	Messages        []Message
	Model           string   // Optional override of the configured model.
	Temperature     *float64 // nil uses the provider default; zero is a valid value.
	MaxOutputTokens int      // Zero uses the provider default.
}
type Usage struct{ InputTokens, OutputTokens, TotalTokens int }
type Response struct {
	Text         string
	Model        string
	FinishReason string // Native provider finish reason/status.
	Usage        Usage
}

// Client implementations must support concurrent calls. Conversation history is
// supplied by callers; clients do not retain user messages.
type Client interface {
	Generate(context.Context, Request) (Response, error)
}

var (
	ErrNotConfigured = errors.New("llm: no default client configured")
	ErrEmptyResponse = errors.New("llm: provider returned no text")
)

type ProviderConfig struct {
	Provider       string `yaml:"provider"`
	Model          string `yaml:"model"`
	BaseURL        string `yaml:"base_url"` // API root, including /v1 or /v1beta.
	APIKey         string `yaml:"api_key" json:"-"`
	APIKeyEnv      string `yaml:"api_key_env"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}
type Config struct {
	Default   string                    `yaml:"default"`
	Providers map[string]ProviderConfig `yaml:"providers"`
}

type provider struct {
	kind, model, baseURL, key string
	http                      *http.Client
}

// New returns a standalone provider without changing global configuration.
// A supplied HTTP client's transport is reused; a finite timeout is always set.
func New(cfg ProviderConfig, httpClient *http.Client) (Client, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Provider))
	var base string
	switch kind {
	case "chatgpt", "openai":
		kind = "chatgpt"
		base = "https://api.openai.com/v1"
	case "deepseek":
		base = "https://api.deepseek.com"
	case "gemini":
		base = "https://generativelanguage.googleapis.com/v1beta"
	default:
		return nil, fmt.Errorf("llm: unsupported provider %q", cfg.Provider)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: model is required")
	}
	key := cfg.APIKey
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("llm: API key is required")
	}
	if cfg.BaseURL != "" {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("llm: invalid API base URL")
	}
	if cfg.TimeoutSeconds < 0 || cfg.TimeoutSeconds > 3600 {
		return nil, errors.New("llm: timeout_seconds must be between 0 and 3600")
	}
	hc := http.Client{Timeout: 60 * time.Second}
	if httpClient != nil {
		hc = *httpClient
		if hc.Timeout <= 0 {
			hc.Timeout = 60 * time.Second
		}
	}
	if cfg.TimeoutSeconds > 0 {
		hc.Timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	return &provider{kind: kind, model: cfg.Model, baseURL: base, key: key, http: &hc}, nil
}

func (p *provider) Generate(ctx context.Context, req Request) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, errors.New("llm: messages are required")
	}
	for _, m := range req.Messages {
		if m.Role != RoleSystem && m.Role != RoleUser && m.Role != RoleAssistant {
			return Response{}, fmt.Errorf("llm: unsupported role %q", m.Role)
		}
		if strings.TrimSpace(m.Content) == "" {
			return Response{}, errors.New("llm: message content is empty")
		}
	}
	if req.MaxOutputTokens < 0 {
		return Response{}, errors.New("llm: max output tokens cannot be negative")
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2 || *req.Temperature != *req.Temperature) {
		return Response{}, errors.New("llm: temperature must be between 0 and 2")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = p.model
	}
	switch p.kind {
	case "deepseek":
		return p.deepseek(ctx, req)
	case "gemini":
		return p.gemini(ctx, req)
	default:
		return p.chatgpt(ctx, req)
	}
}

var shared = struct {
	sync.RWMutex
	clients     map[string]Client
	defaultName string
}{}

// Configure atomically replaces the global registry after validating every entry.
// An empty configuration disables LLM functionality without affecting other plugins.
func Configure(cfg Config) error {
	clients := make(map[string]Client, len(cfg.Providers))
	if len(cfg.Providers) > 0 && cfg.Default == "" {
		return errors.New("llm: default provider name is required")
	}
	for name, pc := range cfg.Providers {
		if strings.TrimSpace(name) == "" {
			return errors.New("llm: provider name cannot be empty")
		}
		c, err := New(pc, nil)
		if err != nil {
			return fmt.Errorf("llm: provider %q: %w", name, err)
		}
		clients[name] = c
	}
	if cfg.Default != "" && clients[cfg.Default] == nil {
		return errors.New("llm: default provider does not exist")
	}
	shared.Lock()
	shared.clients, shared.defaultName = clients, cfg.Default
	shared.Unlock()
	return nil
}
func Get(name string) (Client, error) {
	shared.RLock()
	defer shared.RUnlock()
	if name == "" {
		name = shared.defaultName
	}
	c := shared.clients[name]
	if c == nil {
		if name == "" {
			return nil, ErrNotConfigured
		}
		return nil, fmt.Errorf("llm: client %q is not configured", name)
	}
	return c, nil
}

// Generate calls the current global default client.
func Generate(ctx context.Context, req Request) (Response, error) {
	c, err := Get("")
	if err != nil {
		return Response{}, err
	}
	return c.Generate(ctx, req)
}
