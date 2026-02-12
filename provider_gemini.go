package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

type GeminiProvider struct {
	client *genai.Client
	model  string
	cfg    Config
}

func NewGeminiProvider(cfg Config) (*GeminiProvider, error) {
	pc := cfg.ProviderCfg("gemini")
	if pc.APIKey == "" {
		return nil, fmt.Errorf("gemini api_key not set (set GEMINI_API_KEY or providers.gemini.api_key in config)")
	}
	clientCfg := &genai.ClientConfig{
		APIKey:  pc.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if pc.URL != "" {
		clientCfg.HTTPOptions = genai.HTTPOptions{BaseURL: pc.URL}
	}
	client, err := genai.NewClient(context.Background(), clientCfg)
	if err != nil {
		return nil, fmt.Errorf("creating gemini client: %w", err)
	}
	return &GeminiProvider{client: client, model: pc.Model, cfg: cfg}, nil
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) MaxContext() int {
	if strings.Contains(p.model, "pro") {
		return 2000000
	}
	return 1000000
}

func (p *GeminiProvider) SendStream(ctx context.Context, msgs []Message, tools []ToolDef, systemPrompt string) (<-chan StreamChunk, error) {
	contents := convertToGeminiContents(msgs)
	geminiTools := convertToGeminiTools(tools)

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(systemPrompt)},
		},
	}

	if p.cfg.MaxTokens > 0 {
		config.MaxOutputTokens = int32(p.cfg.MaxTokens)
	}

	if len(geminiTools) > 0 {
		config.Tools = []*genai.Tool{
			{FunctionDeclarations: geminiTools},
		}
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		var usage *Usage
		toolCallIndex := 0

		for result, err := range p.client.Models.GenerateContentStream(ctx, p.model, contents, config) {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				ch <- StreamChunk{Err: err}
				return
			}

			if len(result.Candidates) > 0 {
				candidate := result.Candidates[0]
				if candidate.Content != nil {
					for _, part := range candidate.Content.Parts {
						if part.Text != "" {
							ch <- StreamChunk{Text: part.Text}
						}
						if part.FunctionCall != nil {
							fc := part.FunctionCall
							argsJSON, _ := json.Marshal(fc.Args)
							ch <- StreamChunk{
								ToolCallDelta: &ToolCallDelta{
									Index: toolCallIndex,
									ID:    fc.ID,
									Name:  fc.Name,
									Args:  string(argsJSON),
								},
							}
							toolCallIndex++
						}
					}
				}
			}

			if result.UsageMetadata != nil {
				usage = &Usage{
					InputTokens:  int(result.UsageMetadata.PromptTokenCount),
					OutputTokens: int(result.UsageMetadata.CandidatesTokenCount),
				}
			}
		}

		ch <- StreamChunk{Done: true, Usage: usage}
	}()

	return ch, nil
}

func convertToGeminiContents(msgs []Message) []*genai.Content {
	var result []*genai.Content

	for _, m := range msgs {
		switch m.Role {
		case "user":
			result = append(result, &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{genai.NewPartFromText(m.Content)},
			})
		case "assistant":
			content := &genai.Content{Role: "model"}
			if m.Content != "" {
				content.Parts = append(content.Parts, genai.NewPartFromText(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				json.Unmarshal(tc.Args, &args)
				content.Parts = append(content.Parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						Name: tc.Name,
						Args: args,
						ID:   tc.ID,
					},
				})
			}
			result = append(result, content)
		case "tool":
			var response map[string]any
			// Try to parse as JSON, fall back to wrapping in object
			if err := json.Unmarshal([]byte(m.Content), &response); err != nil {
				response = map[string]any{"result": m.Content}
			}
			result = append(result, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					{
						FunctionResponse: &genai.FunctionResponse{
							Name:     findToolName(msgs, m.ToolCallID),
							ID:       m.ToolCallID,
							Response: response,
						},
					},
				},
			})
		}
	}

	return result
}

func findToolName(msgs []Message, toolCallID string) string {
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID == toolCallID {
				return tc.Name
			}
		}
	}
	return ""
}

func convertToGeminiTools(tools []ToolDef) []*genai.FunctionDeclaration {
	var result []*genai.FunctionDeclaration
	for _, t := range tools {
		fd := &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
		}

		if props, ok := t.Parameters["properties"].(map[string]any); ok {
			schema := &genai.Schema{
				Type:       genai.TypeObject,
				Properties: make(map[string]*genai.Schema),
			}
			for name, v := range props {
				if propMap, ok := v.(map[string]any); ok {
					prop := &genai.Schema{}
					if tp, ok := propMap["type"].(string); ok {
						switch tp {
						case "string":
							prop.Type = genai.TypeString
						case "integer":
							prop.Type = genai.TypeInteger
						case "number":
							prop.Type = genai.TypeNumber
						case "boolean":
							prop.Type = genai.TypeBoolean
						case "array":
							prop.Type = genai.TypeArray
						default:
							prop.Type = genai.TypeString
						}
					}
					if d, ok := propMap["description"].(string); ok {
						prop.Description = d
					}
					schema.Properties[name] = prop
				}
			}
			if req, ok := t.Parameters["required"].([]string); ok {
				schema.Required = req
			}
			if req, ok := t.Parameters["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						schema.Required = append(schema.Required, s)
					}
				}
			}
			fd.Parameters = schema
		}

		result = append(result, fd)
	}
	return result
}
