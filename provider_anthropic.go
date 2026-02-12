package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liushuangls/go-anthropic/v2"
	"github.com/liushuangls/go-anthropic/v2/jsonschema"
)

type AnthropicProvider struct {
	client *anthropic.Client
	model  string
	cfg    Config
}

func NewAnthropicProvider(cfg Config) (*AnthropicProvider, error) {
	pc := cfg.ProviderCfg("anthropic")
	if pc.APIKey == "" {
		return nil, fmt.Errorf("anthropic api_key not set (set ANTHROPIC_API_KEY or providers.anthropic.api_key in config)")
	}
	var opts []anthropic.ClientOption
	if pc.URL != "" {
		opts = append(opts, anthropic.WithBaseURL(pc.URL))
	}
	client := anthropic.NewClient(pc.APIKey, opts...)
	return &AnthropicProvider{client: client, model: pc.Model, cfg: cfg}, nil
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) MaxContext() int {
	if strings.Contains(p.model, "opus") || strings.Contains(p.model, "sonnet") || strings.Contains(p.model, "haiku") {
		return 200000
	}
	return 200000
}

func (p *AnthropicProvider) SendStream(ctx context.Context, msgs []Message, tools []ToolDef, systemPrompt string) (<-chan StreamChunk, error) {
	// Convert messages
	anthMsgs := convertToAnthropicMessages(msgs)

	// Convert tools
	anthTools := convertToAnthropicTools(tools)

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)

		// Track tool calls being built
		type toolCallState struct {
			id   string
			name string
			args strings.Builder
		}
		toolCalls := make(map[int]*toolCallState)
		var inputTokens, outputTokens int

		req := anthropic.MessagesStreamRequest{
			MessagesRequest: anthropic.MessagesRequest{
				Model:     anthropic.Model(p.model),
				Messages:  anthMsgs,
				MaxTokens: p.cfg.MaxTokens,
				System:    systemPrompt,
			},
			OnMessageStart: func(data anthropic.MessagesEventMessageStartData) {
				inputTokens = data.Message.Usage.InputTokens
			},
			OnContentBlockStart: func(data anthropic.MessagesEventContentBlockStartData) {
				if data.ContentBlock.Type == anthropic.MessagesContentTypeToolUse {
					toolCalls[data.Index] = &toolCallState{
						id:   data.ContentBlock.MessageContentToolUse.ID,
						name: data.ContentBlock.MessageContentToolUse.Name,
					}
					ch <- StreamChunk{
						ToolCallDelta: &ToolCallDelta{
							Index: data.Index,
							ID:    data.ContentBlock.MessageContentToolUse.ID,
							Name:  data.ContentBlock.MessageContentToolUse.Name,
						},
					}
				}
			},
			OnContentBlockDelta: func(data anthropic.MessagesEventContentBlockDeltaData) {
				if ctx.Err() != nil {
					return
				}
				switch data.Delta.Type {
				case anthropic.MessagesContentTypeTextDelta:
					text := data.Delta.GetText()
					ch <- StreamChunk{Text: text}
				case anthropic.MessagesContentTypeInputJsonDelta:
					if data.Delta.PartialJson != nil {
						if tc, ok := toolCalls[data.Index]; ok {
							tc.args.WriteString(*data.Delta.PartialJson)
						}
						ch <- StreamChunk{
							ToolCallDelta: &ToolCallDelta{
								Index: data.Index,
								Args:  *data.Delta.PartialJson,
							},
						}
					}
				}
			},
			OnMessageDelta: func(data anthropic.MessagesEventMessageDeltaData) {
				outputTokens = data.Usage.OutputTokens
			},
		}

		if len(anthTools) > 0 {
			req.MessagesRequest.Tools = anthTools
		}

		_, err := p.client.CreateMessagesStream(ctx, req)
		if err != nil {
			ch <- StreamChunk{Err: err}
			return
		}

		ch <- StreamChunk{
			Done: true,
			Usage: &Usage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			},
		}
	}()

	return ch, nil
}

func convertToAnthropicMessages(msgs []Message) []anthropic.Message {
	var result []anthropic.Message

	for _, m := range msgs {
		switch m.Role {
		case "user":
			content := m.Content
			if content == "" {
				content = " "
			}
			result = append(result, anthropic.NewUserTextMessage(content))
		case "assistant":
			var content []anthropic.MessageContent
			if m.Content != "" {
				content = append(content, anthropic.MessageContent{
					Type: anthropic.MessagesContentTypeText,
					Text: &m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				content = append(content, anthropic.MessageContent{
					Type: anthropic.MessagesContentTypeToolUse,
					MessageContentToolUse: &anthropic.MessageContentToolUse{
						ID:    tc.ID,
						Name:  tc.Name,
						Input: json.RawMessage(tc.Args),
					},
				})
			}
			if len(content) == 0 {
				continue // skip empty assistant messages
			}
			result = append(result, anthropic.Message{
				Role:    anthropic.RoleAssistant,
				Content: content,
			})
		case "tool":
			toolContent := m.Content
			if toolContent == "" {
				toolContent = "(no output)"
			}
			result = append(result, anthropic.NewToolResultsMessage(m.ToolCallID, toolContent, false))
		}
	}

	return result
}

func convertToAnthropicTools(tools []ToolDef) []anthropic.ToolDefinition {
	var result []anthropic.ToolDefinition
	for _, t := range tools {
		schema := convertToJSONSchema(t.Parameters)
		result = append(result, anthropic.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return result
}

func convertToJSONSchema(params map[string]any) jsonschema.Definition {
	def := jsonschema.Definition{
		Type: jsonschema.Object,
	}

	if props, ok := params["properties"].(map[string]any); ok {
		def.Properties = make(map[string]jsonschema.Definition)
		for name, v := range props {
			if propMap, ok := v.(map[string]any); ok {
				prop := jsonschema.Definition{}
				if t, ok := propMap["type"].(string); ok {
					prop.Type = jsonschema.DataType(t)
				}
				if d, ok := propMap["description"].(string); ok {
					prop.Description = d
				}
				def.Properties[name] = prop
			}
		}
	}

	if req, ok := params["required"].([]string); ok {
		def.Required = req
	}
	// Also handle []any (from JSON unmarshaling)
	if req, ok := params["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				def.Required = append(def.Required, s)
			}
		}
	}

	return def
}
