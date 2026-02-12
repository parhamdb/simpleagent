package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type BedrockProvider struct {
	client *bedrockruntime.Client
	model  string
	cfg    Config
}

func NewBedrockProvider(cfg Config) (*BedrockProvider, error) {
	pc := cfg.ProviderCfg("bedrock")
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	client := bedrockruntime.NewFromConfig(awsCfg)
	return &BedrockProvider{client: client, model: pc.Model, cfg: cfg}, nil
}

func (p *BedrockProvider) Name() string { return "bedrock" }

func (p *BedrockProvider) MaxContext() int { return 200000 }

func (p *BedrockProvider) SendStream(ctx context.Context, msgs []Message, tools []ToolDef, systemPrompt string) (<-chan StreamChunk, error) {
	bedrockMsgs := convertToBedrockMessages(msgs)
	bedrockTools := convertToBedrockTools(tools)

	input := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(p.model),
		Messages: bedrockMsgs,
		System: []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{Value: systemPrompt},
		},
	}

	if p.cfg.MaxTokens > 0 {
		input.InferenceConfig = &types.InferenceConfiguration{
			MaxTokens: aws.Int32(int32(p.cfg.MaxTokens)),
		}
	}

	if len(bedrockTools) > 0 {
		input.ToolConfig = &types.ToolConfiguration{
			Tools: bedrockTools,
		}
	}

	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 64)

	go func() {
		defer close(ch)
		defer output.GetStream().Close()

		// Track current tool use being built
		type toolState struct {
			id   string
			name string
			args strings.Builder
		}
		var currentTool *toolState
		currentBlockIndex := 0

		for event := range output.GetStream().Events() {
			if ctx.Err() != nil {
				return
			}

			switch v := event.(type) {
			case *types.ConverseStreamOutputMemberContentBlockStart:
				switch start := v.Value.Start.(type) {
				case *types.ContentBlockStartMemberToolUse:
					currentTool = &toolState{
						id:   aws.ToString(start.Value.ToolUseId),
						name: aws.ToString(start.Value.Name),
					}
					ch <- StreamChunk{
						ToolCallDelta: &ToolCallDelta{
							Index: currentBlockIndex,
							ID:    currentTool.id,
							Name:  currentTool.name,
						},
					}
				}

			case *types.ConverseStreamOutputMemberContentBlockDelta:
				switch delta := v.Value.Delta.(type) {
				case *types.ContentBlockDeltaMemberText:
					ch <- StreamChunk{Text: delta.Value}
				case *types.ContentBlockDeltaMemberToolUse:
					if delta.Value.Input != nil && currentTool != nil {
						chunk := aws.ToString(delta.Value.Input)
						currentTool.args.WriteString(chunk)
						ch <- StreamChunk{
							ToolCallDelta: &ToolCallDelta{
								Index: currentBlockIndex,
								Args:  chunk,
							},
						}
					}
				}

			case *types.ConverseStreamOutputMemberContentBlockStop:
				if currentTool != nil {
					currentTool = nil
				}
				currentBlockIndex++

			case *types.ConverseStreamOutputMemberMetadata:
				var usage *Usage
				if v.Value.Usage != nil {
					usage = &Usage{
						InputTokens:  int(aws.ToInt32(v.Value.Usage.InputTokens)),
						OutputTokens: int(aws.ToInt32(v.Value.Usage.OutputTokens)),
					}
				}
				ch <- StreamChunk{Done: true, Usage: usage}
			}
		}

		if err := output.GetStream().Err(); err != nil {
			ch <- StreamChunk{Err: err}
		}
	}()

	return ch, nil
}

func convertToBedrockMessages(msgs []Message) []types.Message {
	var result []types.Message

	for _, m := range msgs {
		switch m.Role {
		case "user":
			result = append(result, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: m.Content},
				},
			})
		case "assistant":
			var content []types.ContentBlock
			if m.Content != "" {
				content = append(content, &types.ContentBlockMemberText{Value: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input map[string]any
				json.Unmarshal(tc.Args, &input)
				if input == nil {
					input = make(map[string]any)
				}
				content = append(content, &types.ContentBlockMemberToolUse{
					Value: types.ToolUseBlock{
						ToolUseId: aws.String(tc.ID),
						Name:      aws.String(tc.Name),
						Input:     document.NewLazyDocument(input),
					},
				})
			}
			result = append(result, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})
		case "tool":
			result = append(result, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolResult{
						Value: types.ToolResultBlock{
							ToolUseId: aws.String(m.ToolCallID),
							Content: []types.ToolResultContentBlock{
								&types.ToolResultContentBlockMemberText{Value: m.Content},
							},
							Status: types.ToolResultStatusSuccess,
						},
					},
				},
			})
		}
	}

	return result
}

func convertToBedrockTools(tools []ToolDef) []types.Tool {
	var result []types.Tool
	for _, t := range tools {
		result = append(result, &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(t.Name),
				Description: aws.String(t.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(t.Parameters),
				},
			},
		})
	}
	return result
}
