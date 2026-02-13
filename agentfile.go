package main

import (
	"os"
	"strings"
)

// AgentFile represents a parsed .agent file.
// Format: optional --- delimited frontmatter with key: value pairs, body = system prompt.
type AgentFile struct {
	Path        string
	Description string
	Deny        []string
	Allow       []string
	Model       string
	Provider    string
	URL         string
	Prompt      string
}

// ParseAgentFile reads and parses an .agent file.
// Frontmatter (between --- lines) contains key: value pairs.
// Everything after frontmatter is the system prompt.
// If no frontmatter, entire file is the prompt.
func ParseAgentFile(path string) (*AgentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	af := &AgentFile{Path: path}
	content := string(data)

	// Strip shebang line (#!/usr/bin/env simpleagent)
	if strings.HasPrefix(content, "#!") {
		if idx := strings.IndexByte(content, '\n'); idx >= 0 {
			content = content[idx+1:]
		}
	}

	// Check for frontmatter
	if strings.HasPrefix(strings.TrimSpace(content), "---") {
		// Find the opening ---
		start := strings.Index(content, "---")
		rest := content[start+3:]

		// Find closing ---
		end := strings.Index(rest, "---")
		if end >= 0 {
			frontmatter := rest[:end]
			af.Prompt = strings.TrimSpace(rest[end+3:])
			parseFrontmatter(frontmatter, af)
		} else {
			// No closing ---, treat entire file as prompt
			af.Prompt = strings.TrimSpace(content)
		}
	} else {
		af.Prompt = strings.TrimSpace(content)
	}

	return af, nil
}

func parseFrontmatter(fm string, af *AgentFile) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch key {
		case "description":
			af.Description = val
		case "deny":
			af.Deny = splitCSV(val)
		case "allow":
			af.Allow = splitCSV(val)
		case "model":
			af.Model = val
		case "provider":
			af.Provider = val
		case "url":
			af.URL = val
		}
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ToolsConfig returns a ToolsConfig from the agent file's deny/allow fields.
func (af *AgentFile) ToolsConfig() ToolsConfig {
	return ToolsConfig{
		Deny:  af.Deny,
		Allow: af.Allow,
	}
}

// BuilderPrompt returns a system prompt for creating new .agent files.
func BuilderPrompt(target string) (agentFile *AgentFile, firstMsg string) {
	prompt := `You help create .agent files through conversation. The format:

` + "```" + `
#!/usr/bin/env simpleagent
---
description: One-line description
deny: tool1, tool2
allow: tool1, tool2
model: model-name
provider: provider-name
url: custom-endpoint-url
---

System prompt goes here.
Skills are markdown sections (# skill: Name).
` + "```" + `

All header fields are optional. The body is what makes the agent — clear instructions the LLM follows.

Available tools agents can use: read_file, write_file, edit_file, list_dir, delete, move, copy, file_info, make_dir, chmod, bash, start_process, write_stdin, read_output, kill_process, list_processes, grep, find_files, diff, patch, ask_user.

WORKFLOW:
1. Ask what this agent should do. If the user already provided a description, ask follow-up questions to flesh it out.
2. Ask about specific skills, tools it needs, and any restrictions (deny/allow).
3. Ask ONE question at a time. Keep it conversational.
4. Do NOT generate the file until the user indicates they're ready (e.g. "done", "that's it", "looks good", "go ahead", "create it").
5. When ready, write the file with write_file. Then ask if they want any changes.
6. If they request changes, apply them with edit_file and ask again.

Be concise in your questions. Don't overwhelm with options — ask naturally.`

	af := &AgentFile{Prompt: prompt}

	if target != "" {
		firstMsg = "Create a new agent file at: " + target
	}
	return af, firstMsg
}

// EditorPrompt returns a system prompt for editing existing .agent files.
func EditorPrompt(target string) (agentFile *AgentFile, firstMsg string) {
	content, err := os.ReadFile(target)
	if err != nil {
		return nil, ""
	}

	prompt := `You are editing the agent file at ` + target + `. Help the user modify it through conversation.

Current contents:
` + "```" + `
` + string(content) + `
` + "```" + `

WORKFLOW:
1. Briefly summarize what this agent currently does, then ask what the user wants to change.
2. After each change, apply it with edit_file (or write_file for full rewrites). Preserve the shebang and frontmatter format.
3. After applying a change, ask if there's anything else to modify.
4. Keep going until the user says they're done.

Be concise. One question at a time.`

	af := &AgentFile{Prompt: prompt}
	firstMsg = "Review this agent file and ask what I'd like to change."
	return af, firstMsg
}
