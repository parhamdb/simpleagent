package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

type Agent struct {
	provider   Provider
	cfg        Config
	session    *Session
	mode       Mode
	tools      *ToolRegistry
	totalUsage Usage
	agentFile  *AgentFile
}

func NewAgent(provider Provider, cfg Config, session *Session, af *AgentFile) *Agent {
	if session == nil {
		session = NewSession(provider.Name(), "")
	}

	// Build tool config: agent file overrides config
	toolsCfg := cfg.Tools
	if af != nil {
		if len(af.Deny) > 0 || len(af.Allow) > 0 {
			toolsCfg = af.ToolsConfig()
		}
	}

	a := &Agent{
		provider:  provider,
		cfg:       cfg,
		session:   session,
		mode:      ModePlan,
		tools:     NewToolRegistry(toolsCfg),
		agentFile: af,
	}

	bashTimeout = cfg.BashTimeout
	initRenderer()

	return a
}

func (a *Agent) systemPrompt() string {
	cwd, _ := os.Getwd()

	var sb strings.Builder

	// Persona: agent file prompt or default
	if a.agentFile != nil && a.agentFile.Prompt != "" {
		sb.WriteString(a.agentFile.Prompt)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("You are simpleagent, a coding assistant running in the user's terminal.\n\n")
	}

	// Always append: working dir, mode, tools, rules, mode instructions, memory
	sb.WriteString("Working directory: " + cwd + "\n")
	sb.WriteString("Current mode: " + a.mode.String() + "\n\n")

	sb.WriteString("Available tools:\n")
	sb.WriteString("  Files: read_file, write_file, edit_file, list_dir, delete, move, copy, file_info, make_dir, chmod\n")
	sb.WriteString("  Exec: bash, start_process, write_stdin, read_output, kill_process, list_processes\n")
	sb.WriteString("  Search: grep, find_files\n")
	sb.WriteString("  Diff: diff, patch\n")
	sb.WriteString("  User: ask_user\n\n")

	sb.WriteString("CRITICAL RULES:\n")
	sb.WriteString("- ACT, don't narrate. NEVER say \"I'll do X\" or \"Let me X\" without immediately calling the tool in the same response. If you need to explore, call list_dir RIGHT NOW — do not just say you will.\n")
	sb.WriteString("- Every response MUST include at least one tool call unless you are answering a pure knowledge question.\n")
	sb.WriteString("- Read files before editing. Use edit_file for small changes, write_file for new files or full rewrites.\n")
	sb.WriteString("- NEVER use bash for servers, watchers, or anything long-running. bash BLOCKS until the command exits. Use start_process instead, then read_output to check it.\n")
	sb.WriteString("- Be concise. No filler. Short text + tool calls.\n")
	sb.WriteString("- When presenting choices, format as numbered options.\n\n")

	if a.mode == ModePlan {
		sb.WriteString("PLAN mode: Use read-only tools (read_file, list_dir, grep, find_files, file_info, diff, ask_user). Write tools are blocked.\n")
		sb.WriteString("Your goal is to GATHER INFORMATION and BUILD A PLAN before any code is written.\n")
		sb.WriteString("- Explore the codebase thoroughly. Read files, search, understand the current state.\n")
		sb.WriteString("- Ask the user about EVERYTHING you're unsure of. Use ask_user liberally. Clarify requirements, preferences, constraints, tech choices, naming, scope.\n")
		sb.WriteString("- Present options as numbered choices when multiple approaches exist.\n")
		sb.WriteString("- Do NOT assume — if you don't know, ASK.\n")
		sb.WriteString("- Summarize your findings and present a clear plan with steps before the user switches to action mode.\n\n")
	} else {
		sb.WriteString("ACTION mode: Full tool access. Execute tasks directly and autonomously.\n")
		sb.WriteString("- Make decisions yourself. Figure things out by reading code, running commands, testing.\n")
		sb.WriteString("- Do NOT ask for confirmation or permission for routine work.\n")
		sb.WriteString("- ONLY use ask_user when something is critical, dangerous, irreversible, or fundamentally ambiguous (e.g. deleting production data, choosing between incompatible architectures, unclear core requirements).\n")
		sb.WriteString("- If you hit an error, debug and fix it yourself. Don't ask the user unless you're truly stuck after multiple attempts.\n\n")
	}

	if mem := loadMemory(); mem != "" {
		sb.WriteString(mem)
	}

	return sb.String()
}

func (a *Agent) prompt() string {
	return fmt.Sprintf("[%s] > ", a.mode)
}

func (a *Agent) RunOnce(input string) {
	a.session.Messages = append(a.session.Messages, Message{Role: "user", Content: input})
	a.runAgentLoop()
}

func (a *Agent) RunLoop() {
	// Set up Ctrl+C handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)

	for {
		// Check if last message needs LLM response (pending tool results)
		if len(a.session.Messages) > 0 {
			last := a.session.Messages[len(a.session.Messages)-1]
			if last.Role == "tool" {
				a.runAgentLoop()
				continue
			}
		}

		fmt.Print(a.prompt())

		input, err := a.readLine()
		if err != nil {
			fmt.Println("Goodbye!")
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle slash commands
		if strings.HasPrefix(input, "/") {
			if a.handleSlashCommand(input) {
				continue
			}
		}

		a.session.Messages = append(a.session.Messages, Message{Role: "user", Content: input})
		a.runAgentLoop()
	}
}

func (a *Agent) runAgentLoop() {
	for {
		ctx, cancel := context.WithCancel(context.Background())

		// Handle Ctrl+C to cancel streaming
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		go func() {
			<-sigCh
			cancel()
			signal.Stop(sigCh)
		}()

		ch, err := a.provider.SendStream(ctx, a.session.Messages, a.tools.Definitions(), a.systemPrompt())
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
			cancel()
			signal.Stop(sigCh)
			return
		}

		assistantMsg, usage := a.consumeStream(ch)
		cancel()
		signal.Stop(sigCh)

		if usage != nil {
			a.totalUsage.InputTokens += usage.InputTokens
			a.totalUsage.OutputTokens += usage.OutputTokens
			a.session.TokensUsed = a.totalUsage.InputTokens + a.totalUsage.OutputTokens
		}

		a.session.Messages = append(a.session.Messages, assistantMsg)

		if len(assistantMsg.ToolCalls) > 0 {
			for _, tc := range assistantMsg.ToolCalls {
				blocked := a.mode == ModePlan && a.tools.IsWriteTool(tc.Name)
				renderToolCall(tc.Name, string(tc.Args), blocked)

				askUserMode = a.mode
				result, err := a.tools.Execute(tc.Name, tc.Args, a.mode)
				if err != nil {
					result = fmt.Sprintf("error: %v", err)
				}

				a.session.Messages = append(a.session.Messages, Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			a.session.Save()
			continue // back to LLM with tool results
		}

		// Plain text response
		if assistantMsg.Content != "" {
			fmt.Println()
		}
		renderContextLine(usage, a.provider.MaxContext())
		a.session.Save()
		return
	}
}

func (a *Agent) consumeStream(ch <-chan StreamChunk) (Message, *Usage) {
	msg := Message{Role: "assistant"}
	var usage *Usage

	// For accumulating tool call deltas
	toolCalls := make(map[int]*ToolCall)

	for chunk := range ch {
		if chunk.Err != nil {
			fmt.Fprintf(os.Stderr, "\nStream error: %v\n", chunk.Err)
			break
		}

		if chunk.Text != "" {
			fmt.Print(chunk.Text)
			msg.Content += chunk.Text
		}

		if chunk.ToolCallDelta != nil {
			d := chunk.ToolCallDelta
			tc, ok := toolCalls[d.Index]
			if !ok {
				tc = &ToolCall{}
				toolCalls[d.Index] = tc
			}
			if d.ID != "" {
				tc.ID = d.ID
			}
			if d.Name != "" {
				tc.Name = d.Name
			}
			if d.Args != "" {
				tc.Args = append(tc.Args, []byte(d.Args)...)
			}
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}

	// Collect tool calls in order, auto-generate IDs if missing
	for i := 0; i < len(toolCalls); i++ {
		if tc, ok := toolCalls[i]; ok {
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("call_%d", i)
			}
			msg.ToolCalls = append(msg.ToolCalls, *tc)
		}
	}

	return msg, usage
}

func (a *Agent) handleSlashCommand(input string) bool {
	parts := strings.SplitN(input, " ", 2)
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "/exit", "/quit":
		fmt.Println("Goodbye!")
		os.Exit(0)
	case "/plan":
		a.mode = ModePlan
		fmt.Println("Switched to PLAN mode.")
	case "/action":
		a.mode = ModeAction
		fmt.Println("Switched to ACTION mode.")
	case "/new":
		a.session.Save()
		a.session = NewSession(a.provider.Name(), "")
		a.totalUsage = Usage{}
		fmt.Println("Started new session.")
	case "/rename":
		if arg == "" {
			fmt.Println("Usage: /rename <name>")
		} else {
			renameSession(a.session.ID, arg)
			fmt.Printf("Session renamed to %q.\n", arg)
		}
	case "/sessions":
		listAllSessions()
	case "/compact":
		a.compactSession()
	case "/model":
		if arg == "" {
			pc := a.cfg.ProviderCfg(a.cfg.Provider)
			fmt.Printf("Current model: %s\n", pc.Model)
		} else {
			pc := a.cfg.Providers[a.cfg.Provider]
			pc.Model = arg
			a.cfg.Providers[a.cfg.Provider] = pc
			newProvider, err := NewProvider(a.cfg.Provider, a.cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				a.provider = newProvider
				fmt.Printf("Model switched to %s.\n", arg)
			}
		}
	case "/provider":
		if arg == "" {
			fmt.Printf("Current provider: %s\n", a.provider.Name())
		} else {
			a.cfg.Provider = arg
			newProvider, err := NewProvider(arg, a.cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			} else {
				a.provider = newProvider
				fmt.Printf("Provider switched to %s.\n", arg)
			}
		}
	case "/memory":
		if arg == "" {
			fmt.Println("Usage: /memory <text to remember>")
		} else {
			if err := appendMemory(arg); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving memory: %v\n", err)
			} else {
				fmt.Println("Memory saved.")
			}
		}
	case "/help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s (try /help)\n", cmd)
	}
	return true
}

func (a *Agent) compactSession() {
	fmt.Println("Compacting session...")

	compactPrompt := "Summarize the entire conversation so far into a concise summary that preserves all important context, decisions made, code changes, and current state. This summary will replace the conversation history."

	a.session.Messages = append(a.session.Messages, Message{Role: "user", Content: compactPrompt})

	ctx := context.Background()
	ch, err := a.provider.SendStream(ctx, a.session.Messages, nil, a.systemPrompt())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	msg, _ := a.consumeStream(ch)

	// Replace history with summary
	a.session.Messages = []Message{
		{Role: "user", Content: "Previous conversation summary:"},
		{Role: "assistant", Content: msg.Content},
	}
	a.session.Save()
	fmt.Println("\nSession compacted.")
}

func printHelp() {
	fmt.Println(`Commands:
  /plan          Switch to plan mode (read-only)
  /action        Switch to action mode (full access)
  /new           Start a new session
  /rename <name> Name the current session
  /sessions      List all sessions
  /compact       Compress conversation history
  /model <name>  Switch model
  /provider <n>  Switch provider
  /memory <text> Save a note to memory
  /help          Show this help
  /exit          Quit

Keys:
  Shift+Tab      Toggle plan/action mode
  Ctrl+C         Interrupt streaming or exit
  Ctrl+D         Exit`)
}
