package main

import "encoding/json"

type Mode int

const (
	ModePlan Mode = iota
	ModeAction
)

func (m Mode) String() string {
	if m == ModePlan {
		return "PLAN"
	}
	return "ACTION"
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ToolCallDelta struct {
	Index int
	ID    string
	Name  string
	Args  string // incremental JSON fragment
}

type StreamChunk struct {
	Text          string
	ToolCallDelta *ToolCallDelta
	Done          bool
	Err           error
	Usage         *Usage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
