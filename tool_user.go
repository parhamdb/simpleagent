package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func registerUserTools(r *ToolRegistry) {
	r.Register(ToolDef{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "Question to ask the user"},
			},
			"required": []string{"question"},
		},
	}, toolAskUser, false)
}

// askUserMode is set by the agent to control ask_user behavior
var askUserMode Mode = ModePlan

func toolAskUser(args json.RawMessage) (string, error) {
	var params struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// In action mode, auto-proceed
	if askUserMode == ModeAction {
		return "proceed", nil
	}

	fmt.Printf("\n%s\n> ", params.Question)

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	return "no response", nil
}
