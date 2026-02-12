package main

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

type OpenAIProvider struct {
	client  *openai.Client
	backend string
	model   string
	cfg     Config
}

func NewOpenAIProvider(backend string, cfg Config) (*OpenAIProvider, error) {
	pc := cfg.ProviderCfg(backend)
	var opts []option.RequestOption

	switch backend {
	case "openai":
		if pc.APIKey == "" {
			return nil, fmt.Errorf("openai api_key not set (set OPENAI_API_KEY or providers.openai.api_key in config)")
		}
		opts = append(opts, option.WithAPIKey(pc.APIKey))
		if pc.URL != "" {
			opts = append(opts, option.WithBaseURL(pc.URL))
		}
	case "openrouter":
		if pc.APIKey == "" {
			return nil, fmt.Errorf("openrouter api_key not set (set OPENROUTER_API_KEY or providers.openrouter.api_key in config)")
		}
		opts = append(opts, option.WithAPIKey(pc.APIKey))
		url := pc.URL
		if url == "" {
			url = "https://openrouter.ai/api/v1"
		}
		opts = append(opts, option.WithBaseURL(url))
	case "ollama":
		url := pc.URL
		if url == "" {
			url = "http://localhost:11434"
		}
		opts = append(opts, option.WithBaseURL(url+"/v1/"))
		opts = append(opts, option.WithAPIKey("ollama"))
	default:
		return nil, fmt.Errorf("unsupported openai-compatible backend: %s", backend)
	}

	client := openai.NewClient(opts...)
	return &OpenAIProvider{client: &client, backend: backend, model: pc.Model, cfg: cfg}, nil
}

func (p *OpenAIProvider) Name() string { return p.backend }

func (p *OpenAIProvider) MaxContext() int {
	switch p.backend {
	case "openrouter":
		return 200000
	case "ollama":
		return 32000
	default:
		return 128000
	}
}

func (p *OpenAIProvider) SendStream(ctx context.Context, msgs []Message, tools []ToolDef, systemPrompt string) (<-chan StreamChunk, error) {
	oaiMsgs := convertToOpenAIMessages(msgs, systemPrompt)
	oaiTools := convertToOpenAITools(tools)

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		params := openai.ChatCompletionNewParams{
			Model:    p.model,
			Messages: oaiMsgs,
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: param.NewOpt(true),
			},
		}

		if p.cfg.MaxTokens > 0 {
			params.MaxTokens = param.NewOpt(int64(p.cfg.MaxTokens))
		}

		if len(oaiTools) > 0 {
			params.Tools = oaiTools
		}

		stream := p.client.Chat.Completions.NewStreaming(ctx, params)
		defer stream.Close()

		acc := &openai.ChatCompletionAccumulator{}

		for stream.Next() {
			if ctx.Err() != nil {
				return
			}

			chunk := stream.Current()
			acc.AddChunk(chunk)

			// Handle text deltas
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- StreamChunk{Text: choice.Delta.Content}
				}

				// Handle tool call deltas
				for _, tc := range choice.Delta.ToolCalls {
					ch <- StreamChunk{
						ToolCallDelta: &ToolCallDelta{
							Index: int(tc.Index),
							ID:    tc.ID,
							Name:  tc.Function.Name,
							Args:  tc.Function.Arguments,
						},
					}
				}
			}

			// Check for finished tool calls
			if _, ok := acc.JustFinishedToolCall(); ok {
				// Tool call completed, will be collected at the end
			}
		}

		if err := stream.Err(); err != nil {
			ch <- StreamChunk{Err: err}
			return
		}

		// Extract usage from accumulator
		var usage *Usage
		if acc.Usage.TotalTokens > 0 {
			usage = &Usage{
				InputTokens:  int(acc.Usage.PromptTokens),
				OutputTokens: int(acc.Usage.CompletionTokens),
			}
		}

		ch <- StreamChunk{Done: true, Usage: usage}
	}()

	return ch, nil
}

func convertToOpenAIMessages(msgs []Message, systemPrompt string) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion

	// Add system message
	result = append(result, openai.ChatCompletionMessageParamUnion{
		OfSystem: &openai.ChatCompletionSystemMessageParam{
			Content: openai.ChatCompletionSystemMessageParamContentUnion{
				OfString: param.NewOpt(systemPrompt),
			},
		},
	})

	for _, m := range msgs {
		switch m.Role {
		case "user":
			result = append(result, openai.ChatCompletionMessageParamUnion{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt(m.Content),
					},
				},
			})
		case "assistant":
			asstMsg := &openai.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				asstMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: param.NewOpt(m.Content),
				}
			}
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					asstMsg.ToolCalls = append(asstMsg.ToolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: string(tc.Args),
						},
					})
				}
			}
			result = append(result, openai.ChatCompletionMessageParamUnion{
				OfAssistant: asstMsg,
			})
		case "tool":
			result = append(result, openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					ToolCallID: m.ToolCallID,
					Content: openai.ChatCompletionToolMessageParamContentUnion{
						OfString: param.NewOpt(m.Content),
					},
				},
			})
		}
	}

	return result
}

func convertToOpenAITools(tools []ToolDef) []openai.ChatCompletionToolParam {
	var result []openai.ChatCompletionToolParam
	for _, t := range tools {
		result = append(result, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				Parameters:  shared.FunctionParameters(t.Parameters),
			},
		})
	}
	return result
}

func init() {
	// Ensure interfaces are satisfied
	var _ Provider = (*OpenAIProvider)(nil)
}
