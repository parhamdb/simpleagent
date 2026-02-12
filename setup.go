package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type providerOption struct {
	name    string
	label   string
	needKey bool
	envHint string
}

var providerMenu = []providerOption{
	{"anthropic", "anthropic    (Claude — recommended)", true, "ANTHROPIC_API_KEY"},
	{"openai", "openai       (GPT-4o)", true, "OPENAI_API_KEY"},
	{"gemini", "gemini       (Gemini)", true, "GEMINI_API_KEY"},
	{"openrouter", "openrouter   (Multi-model gateway)", true, "OPENROUTER_API_KEY"},
	{"ollama", "ollama       (Local — no API key needed)", false, ""},
	{"bedrock", "bedrock      (AWS — uses env credentials)", false, ""},
}

// runSetupWizard walks the user through provider configuration.
// Returns true if config was saved, false if cancelled.
func runSetupWizard(cfg *Config) bool {
	scanner := bufio.NewScanner(os.Stdin)

	// Provider selection
	fmt.Println("  Provider:")
	fmt.Println()
	for i, p := range providerMenu {
		fmt.Printf("    %d. %s\n", i+1, p.label)
	}
	fmt.Println()

	fmt.Print("  Choice [1]: ")
	choice := 1
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 || n > len(providerMenu) {
				fmt.Fprintln(os.Stderr, "  Invalid choice.")
				return false
			}
			choice = n
		}
	} else {
		return false // Ctrl+D
	}

	selected := providerMenu[choice-1]
	cfg.Provider = selected.name
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}
	pc := cfg.ProviderCfg(selected.name)

	// API key (for providers that need one)
	if selected.needKey {
		fmt.Println()
		fmt.Print("  API key: ")
		if !scanner.Scan() {
			return false
		}
		key := strings.TrimSpace(scanner.Text())
		if key == "" {
			fmt.Fprintln(os.Stderr, "  API key required.")
			return false
		}
		pc.APIKey = key
		fmt.Printf("  Key:     %s\n", maskKey(key))
	}

	// URL (for ollama)
	if selected.name == "ollama" {
		defaultURL := pc.URL
		if defaultURL == "" {
			defaultURL = "http://localhost:11434"
		}
		fmt.Println()
		fmt.Printf("  URL [%s]: ", defaultURL)
		if scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text != "" {
				pc.URL = text
			} else {
				pc.URL = defaultURL
			}
		} else {
			return false
		}
	}

	// Model
	defaultModel := pc.Model
	if defaultModel == "" {
		// Pull from defaults
		defaultModel = DefaultConfig().ProviderCfg(selected.name).Model
	}
	fmt.Println()
	fmt.Printf("  Model [%s]: ", defaultModel)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			pc.Model = text
		} else {
			pc.Model = defaultModel
		}
	} else {
		return false
	}

	// Save — only write provider + active provider entry (minimal config)
	cfg.Providers[selected.name] = pc
	saveCfg := Config{
		Provider:  selected.name,
		Providers: map[string]ProviderConfig{selected.name: pc},
	}
	path := UserConfigPath()
	if err := SaveConfig(path, saveCfg); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Error saving config: %v\n", err)
		return false
	}

	fmt.Printf("\n  Saved to %s\n\n", path)
	return true
}

// maskKey shows the first 8 and last 4 characters of a key.
func maskKey(key string) string {
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:8] + strings.Repeat("*", len(key)-12) + key[len(key)-4:]
}
