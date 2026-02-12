package main

import (
	"context"
	"fmt"
)

type Provider interface {
	Name() string
	SendStream(ctx context.Context, msgs []Message, tools []ToolDef, systemPrompt string) (<-chan StreamChunk, error)
	MaxContext() int
}

func NewProvider(name string, cfg Config) (Provider, error) {
	switch name {
	case "anthropic":
		return NewAnthropicProvider(cfg)
	case "openai":
		return NewOpenAIProvider("openai", cfg)
	case "openrouter":
		return NewOpenAIProvider("openrouter", cfg)
	case "ollama":
		return NewOpenAIProvider("ollama", cfg)
	case "gemini":
		return NewGeminiProvider(cfg)
	case "bedrock":
		return NewBedrockProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}
