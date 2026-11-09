package llm

import "context"

func (p *provider) deepseek(ctx context.Context, req Request) (Response, error) {
	body := struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Temperature *float64  `json:"temperature,omitempty"`
		MaxTokens   int       `json:"max_tokens,omitempty"`
	}{req.Model, req.Messages, req.Temperature, req.MaxOutputTokens}
	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
			Total  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/chat/completions", body, &result); err != nil {
		return Response{}, err
	}
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return Response{}, ErrEmptyResponse
	}
	return Response{Text: result.Choices[0].Message.Content, Model: result.Model, FinishReason: result.Choices[0].FinishReason, Usage: Usage{result.Usage.Input, result.Usage.Output, result.Usage.Total}}, nil
}
