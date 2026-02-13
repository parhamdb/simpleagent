package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var version = "dev"

func main() {
	var (
		providerFlag string
		modelFlag    string
		sessionFlag  string
		showVersion  bool
		showSessions bool
		resumeFlag   bool
		newFlag      bool
		editFlag     bool
		setupFlag    bool
	)

	flag.StringVar(&providerFlag, "provider", "", "LLM provider (anthropic, openai, openrouter, gemini, ollama, bedrock)")
	flag.StringVar(&modelFlag, "m", "", "Model name")
	flag.StringVar(&modelFlag, "model", "", "Model name")
	flag.StringVar(&sessionFlag, "session", "", "Resume specific session by ID or name")
	flag.BoolVar(&showVersion, "version", false, "Print version")
	flag.BoolVar(&showSessions, "sessions", false, "List all sessions")
	flag.BoolVar(&resumeFlag, "resume", false, "Resume last session")
	flag.BoolVar(&newFlag, "new", false, "Create a new .agent file")
	flag.BoolVar(&editFlag, "edit", false, "Edit an existing .agent file")
	flag.BoolVar(&setupFlag, "setup", false, "Run setup wizard")
	flag.Parse()

	if showVersion {
		fmt.Printf("simpleagent v%s\n", version)
		os.Exit(0)
	}

	// Load config: defaults → user-wide → project → env
	cfg := LoadConfig()

	// Parse positional args
	var agentFile *AgentFile
	var inlinePrompt string
	args := flag.Args()

	// Extract .agent file target from args (if present)
	var target string
	if len(args) > 0 && strings.HasSuffix(args[0], ".agent") {
		target = args[0]
		args = args[1:]
	}

	// Handle --new and --edit modes
	if newFlag || editFlag {
		if editFlag {
			if target == "" {
				fmt.Fprintln(os.Stderr, "Usage: simpleagent --edit <file.agent>")
				os.Exit(1)
			}
			if _, err := os.Stat(target); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %s not found\n", target)
				os.Exit(1)
			}
			af, firstMsg := EditorPrompt(target)
			if af == nil {
				fmt.Fprintf(os.Stderr, "Error reading %s\n", target)
				os.Exit(1)
			}
			agentFile = af
			if len(args) > 0 {
				inlinePrompt = strings.Join(args, " ")
			} else {
				inlinePrompt = firstMsg
			}
		} else {
			af, firstMsg := BuilderPrompt(target)
			agentFile = af
			if len(args) > 0 {
				// User provided a description: "simpleagent --new foo.agent manage proxmox"
				desc := strings.Join(args, " ")
				if target != "" {
					inlinePrompt = "Create agent file at " + target + ": " + desc
				} else {
					inlinePrompt = "Create an agent file: " + desc
				}
			} else if firstMsg != "" {
				inlinePrompt = firstMsg
			}
		}
	} else if target != "" {
		// Normal run mode: load .agent file directly
		af, err := ParseAgentFile(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading %s: %v\n", target, err)
			os.Exit(1)
		}
		agentFile = af
		fmt.Printf("Agent: %s\n", agentFile.Path)
	}

	// Remaining args as inline prompt (normal mode)
	if inlinePrompt == "" && len(args) > 0 {
		inlinePrompt = strings.Join(args, " ")
	}

	// Resolve agent runtime directory: .simpleagent/<agent-name>/
	if agentFile != nil && agentFile.Path != "" {
		ResolveAgentDir(filepath.Base(agentFile.Path))
	} else if target != "" {
		// --new/--edit: use target name for agentDir
		ResolveAgentDir(filepath.Base(target))
	} else {
		ResolveAgentDir("")
	}

	// Agent file overrides (layer 4)
	cfg.ApplyAgentFile(agentFile)

	// CLI flag overrides (layer 6 — highest priority)
	if providerFlag != "" {
		cfg.Provider = providerFlag
	}
	if modelFlag != "" {
		if cfg.Providers == nil {
			cfg.Providers = make(map[string]ProviderConfig)
		}
		pc := cfg.Providers[cfg.Provider]
		pc.Model = modelFlag
		cfg.Providers[cfg.Provider] = pc
	}

	if showSessions {
		listAllSessions()
		os.Exit(0)
	}

	// Explicit setup
	if setupFlag {
		if !runSetupWizard(&cfg) {
			os.Exit(1)
		}
		// Reload config to get the saved values merged with defaults
		cfg = LoadConfig()
		cfg.ApplyAgentFile(agentFile)
		if providerFlag != "" {
			cfg.Provider = providerFlag
		}
		if modelFlag != "" {
			pc := cfg.Providers[cfg.Provider]
			pc.Model = modelFlag
			cfg.Providers[cfg.Provider] = pc
		}
	}

	// Auto-detect: no usable provider configured
	if !providerReady(cfg) {
		fmt.Println("No provider configured. Let's set one up.")
		fmt.Println()
		if !runSetupWizard(&cfg) {
			os.Exit(1)
		}
		// Reload config to get the saved values merged with defaults
		cfg = LoadConfig()
		cfg.ApplyAgentFile(agentFile)
		if providerFlag != "" {
			cfg.Provider = providerFlag
		}
		if modelFlag != "" {
			pc := cfg.Providers[cfg.Provider]
			pc.Model = modelFlag
			cfg.Providers[cfg.Provider] = pc
		}
	}

	// Create LLM provider
	llm, err := NewProvider(cfg.Provider, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Determine session
	var session *Session

	if sessionFlag != "" {
		session, err = loadSessionByIDOrName(sessionFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
			os.Exit(1)
		}
	} else if resumeFlag {
		session = loadLastSession()
	}

	// Skip session picker for --new/--edit (transient operations)
	if session == nil && inlinePrompt == "" && !newFlag && !editFlag {
		session = sessionPicker()
	}

	// Start agent
	agent := NewAgent(llm, cfg, session, agentFile)

	// --new and --edit always run in action mode (need write tools)
	if newFlag || editFlag {
		agent.mode = ModeAction
		if inlinePrompt != "" {
			agent.session.Messages = append(agent.session.Messages, Message{Role: "user", Content: inlinePrompt})
		}
		agent.RunLoop()
		return
	}

	// Resumed sessions start in action mode, new sessions in plan mode
	if session != nil && len(session.Messages) > 0 {
		agent.mode = ModeAction
	}

	// If inline prompt provided, use it as first message in action mode
	if inlinePrompt != "" {
		agent.mode = ModeAction
		agent.RunOnce(inlinePrompt)
		return
	}

	agent.RunLoop()
}
