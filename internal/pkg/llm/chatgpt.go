package llm

import (
	"context"
	"strings"
)

// ChatGPT uses the OpenAI Responses API; store:false avoids persisting responses.
func (p *provider) chatgpt(ctx context.Context, req Request) (Response, error) {
	body := struct {
		Model           string    `json:"model"`
		Input           []Message `json:"input"`
		Store           bool      `json:"store"`
		Temperature     *float64  `json:"temperature,omitempty"`
		MaxOutputTokens int       `json:"max_output_tokens,omitempty"`
	}{req.Model, req.Messages, false, req.Temperature, req.MaxOutputTokens}
	var result struct {
		Model  string `json:"model"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
			Total  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/responses", body, &result); err != nil {
		return Response{}, err
	}
	var text strings.Builder
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				text.WriteString(content.Text)
			}
		}
	}
	if text.Len() == 0 {
		return Response{}, ErrEmptyResponse
	}
	return Response{Text: text.String(), Model: result.Model, FinishReason: result.Status, Usage: Usage{result.Usage.Input, result.Usage.Output, result.Usage.Total}}, nil
}
