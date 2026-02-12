package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

var agentDir string // .simpleagent/<agent-name>/ — sessions + AGENT.md live here

// ResolveAgentDir sets agentDir in the current working directory.
// .simpleagent/<agent-name>/ for sessions + AGENT.md.
// If no agent file, uses "default" as the subdirectory.
func ResolveAgentDir(agentFileName string) {
	if agentFileName != "" {
		agentDir = filepath.Join(".simpleagent", agentFileName)
	} else {
		agentDir = filepath.Join(".simpleagent", "default")
	}
	agentDir, _ = filepath.Abs(agentDir)
}

// ProviderConfig holds per-provider LLM settings.
type ProviderConfig struct {
	APIKey string `json:"api_key,omitempty"`
	Model  string `json:"model,omitempty"`
	URL    string `json:"url,omitempty"`
}

type ToolsConfig struct {
	Deny  []string `json:"deny"`
	Allow []string `json:"allow"`
}

type Config struct {
	Provider    string                    `json:"provider"`
	Providers   map[string]ProviderConfig `json:"providers"`
	MaxTokens   int                       `json:"max_tokens"`
	BashTimeout int                       `json:"bash_timeout"`
	Tools       ToolsConfig               `json:"tools"`
}

func DefaultConfig() Config {
	return Config{
		Provider: "anthropic",
		Providers: map[string]ProviderConfig{
			"anthropic":  {Model: "claude-sonnet-4-20250514"},
			"openai":     {Model: "gpt-4o"},
			"openrouter": {Model: "anthropic/claude-sonnet-4", URL: "https://openrouter.ai/api/v1"},
			"gemini":     {Model: "gemini-2.5-flash"},
			"ollama":     {Model: "qwen2.5-coder:14b", URL: "http://localhost:11434"},
			"bedrock":    {Model: "anthropic.claude-sonnet-4-20250514-v1:0"},
		},
		MaxTokens:   8192,
		BashTimeout: 120,
	}
}

// ProviderCfg returns the config for a named provider (never nil-like).
func (c Config) ProviderCfg(name string) ProviderConfig {
	if pc, ok := c.Providers[name]; ok {
		return pc
	}
	return ProviderConfig{}
}

// ApplyAgentFile merges .agent file overrides into the config.
func (c *Config) ApplyAgentFile(af *AgentFile) {
	if af == nil {
		return
	}
	if af.Provider != "" {
		c.Provider = af.Provider
	}
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	pc := c.Providers[c.Provider]
	if af.Model != "" {
		pc.Model = af.Model
	}
	if af.URL != "" {
		pc.URL = af.URL
	}
	c.Providers[c.Provider] = pc
}

// LoadConfig builds the final config by cascading layers:
// 1. Hardcoded defaults
// 2. ~/.simpleagent/config.json (user-wide)
// 3. .simpleagent/config.json (project)
// 4. Environment variables
func LoadConfig() Config {
	cfg := DefaultConfig()

	// User-wide config
	if home, err := os.UserHomeDir(); err == nil {
		mergeConfigFile(filepath.Join(home, ".simpleagent", "config.json"), &cfg)
	}

	// Project config (CWD)
	mergeConfigFile(filepath.Join(".simpleagent", "config.json"), &cfg)

	// Env var overrides
	applyEnvOverrides(&cfg)

	return cfg
}

// mergeConfigFile deep-merges a config file into cfg.
// Provider entries are merged field-by-field, not replaced wholesale.
func mergeConfigFile(path string, cfg *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	// First, check for and migrate old-format fields
	migrateOldConfig(data, cfg)

	// Parse into intermediate struct for deep merge
	var raw struct {
		Provider    string                       `json:"provider"`
		Providers   map[string]json.RawMessage   `json:"providers"`
		MaxTokens   *int                         `json:"max_tokens"`
		BashTimeout *int                         `json:"bash_timeout"`
		Tools       *ToolsConfig                 `json:"tools"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	if raw.Provider != "" {
		cfg.Provider = raw.Provider
	}
	if raw.MaxTokens != nil {
		cfg.MaxTokens = *raw.MaxTokens
	}
	if raw.BashTimeout != nil {
		cfg.BashTimeout = *raw.BashTimeout
	}
	if raw.Tools != nil {
		cfg.Tools = *raw.Tools
	}

	// Deep-merge each provider entry
	for name, rawPC := range raw.Providers {
		var pc ProviderConfig
		if err := json.Unmarshal(rawPC, &pc); err != nil {
			continue
		}
		existing := cfg.Providers[name]
		if pc.APIKey != "" {
			existing.APIKey = pc.APIKey
		}
		if pc.Model != "" {
			existing.Model = pc.Model
		}
		if pc.URL != "" {
			existing.URL = pc.URL
		}
		cfg.Providers[name] = existing
	}
}

// migrateOldConfig maps old flat config fields into the new providers structure.
func migrateOldConfig(data []byte, cfg *Config) {
	var old struct {
		AnthropicAPIKey  string            `json:"anthropic_api_key"`
		OpenAIAPIKey     string            `json:"openai_api_key"`
		OpenRouterAPIKey string            `json:"openrouter_api_key"`
		GeminiAPIKey     string            `json:"gemini_api_key"`
		OllamaHost       string            `json:"ollama_host"`
		Model            map[string]string `json:"model"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return
	}

	set := func(name string, pc ProviderConfig) {
		existing := cfg.Providers[name]
		if pc.APIKey != "" {
			existing.APIKey = pc.APIKey
		}
		if pc.Model != "" {
			existing.Model = pc.Model
		}
		if pc.URL != "" {
			existing.URL = pc.URL
		}
		cfg.Providers[name] = existing
	}

	if old.AnthropicAPIKey != "" {
		set("anthropic", ProviderConfig{APIKey: old.AnthropicAPIKey})
	}
	if old.OpenAIAPIKey != "" {
		set("openai", ProviderConfig{APIKey: old.OpenAIAPIKey})
	}
	if old.OpenRouterAPIKey != "" {
		set("openrouter", ProviderConfig{APIKey: old.OpenRouterAPIKey})
	}
	if old.GeminiAPIKey != "" {
		set("gemini", ProviderConfig{APIKey: old.GeminiAPIKey})
	}
	if old.OllamaHost != "" {
		set("ollama", ProviderConfig{URL: old.OllamaHost})
	}

	// Migrate old model map
	for name, model := range old.Model {
		set(name, ProviderConfig{Model: model})
	}
}

// UserConfigPath returns the path to the user-wide config file.
func UserConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".simpleagent", "config.json")
}

// SaveConfig writes a config to the given path, creating directories as needed.
func SaveConfig(path string, cfg Config) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// providerReady returns true if the active provider has enough config to initialize.
func providerReady(cfg Config) bool {
	pc := cfg.ProviderCfg(cfg.Provider)
	switch cfg.Provider {
	case "ollama", "bedrock":
		return true // ollama needs no key, bedrock uses AWS SDK
	default:
		return pc.APIKey != ""
	}
}

func applyEnvOverrides(cfg *Config) {
	envMap := map[string]struct{ provider, field string }{
		"ANTHROPIC_API_KEY":  {"anthropic", "api_key"},
		"OPENAI_API_KEY":     {"openai", "api_key"},
		"OPENROUTER_API_KEY": {"openrouter", "api_key"},
		"GEMINI_API_KEY":     {"gemini", "api_key"},
		"OLLAMA_HOST":        {"ollama", "url"},
	}
	for env, target := range envMap {
		if v := os.Getenv(env); v != "" {
			pc := cfg.Providers[target.provider]
			switch target.field {
			case "api_key":
				pc.APIKey = v
			case "url":
				pc.URL = v
			}
			cfg.Providers[target.provider] = pc
		}
	}

	if v := os.Getenv("SIMPLEAGENT_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxTokens = n
		}
	}
}
