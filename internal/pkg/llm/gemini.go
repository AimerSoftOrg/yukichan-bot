package llm

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

func (p *provider) gemini(ctx context.Context, req Request) (Response, error) {
	body := struct {
		Contents          []geminiContent `json:"contents"`
		SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
		GenerationConfig  struct {
			Temperature     *float64 `json:"temperature,omitempty"`
			MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
		} `json:"generationConfig"`
	}{}
	for _, m := range req.Messages {
		part := geminiPart{Text: m.Content}
		if m.Role == RoleSystem {
			if body.SystemInstruction == nil {
				body.SystemInstruction = &geminiContent{}
			}
			body.SystemInstruction.Parts = append(body.SystemInstruction.Parts, part)
			continue
		}
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		n := len(body.Contents)
		if n > 0 && body.Contents[n-1].Role == role {
			body.Contents[n-1].Parts = append(body.Contents[n-1].Parts, part)
		} else {
			body.Contents = append(body.Contents, geminiContent{Role: role, Parts: []geminiPart{part}})
		}
	}
	if len(body.Contents) == 0 {
		return Response{}, errors.New("llm: Gemini requires at least one user or assistant message")
	}
	body.GenerationConfig.Temperature = req.Temperature
	body.GenerationConfig.MaxOutputTokens = req.MaxOutputTokens
	var result struct {
		Model      string `json:"modelVersion"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Input  int `json:"promptTokenCount"`
			Output int `json:"candidatesTokenCount"`
			Total  int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := p.post(ctx, "/models/"+url.PathEscape(req.Model)+":generateContent", body, &result); err != nil {
		return Response{}, err
	}
	if len(result.Candidates) == 0 {
		return Response{}, ErrEmptyResponse
	}
	var text strings.Builder
	for _, part := range result.Candidates[0].Content.Parts {
		if !part.Thought {
			text.WriteString(part.Text)
		}
	}
	if text.Len() == 0 {
		return Response{}, ErrEmptyResponse
	}
	return Response{Text: text.String(), Model: result.Model, FinishReason: result.Candidates[0].FinishReason, Usage: Usage{result.Usage.Input, result.Usage.Output, result.Usage.Total}}, nil
}
