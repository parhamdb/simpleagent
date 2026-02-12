# simpleagent

Single Go binary LLM agent. No frameworks. One `.agent` file = one agent. Portable, shareable.

## Quick Reference

```bash
make build                           # Build with current VERSION
make release                         # Bump + build + install
simpleagent                          # Interactive, default agent
simpleagent "fix bug"                # One-shot, default agent
simpleagent coder.agent              # Run agent file
simpleagent coder.agent "fix bug"    # Run agent file, one-shot
simpleagent --new proxmox.agent      # Create agent interactively
simpleagent --edit proxmox.agent     # Edit agent interactively
chmod +x proxmox.agent && ./proxmox.agent  # Shebang execution
```

## Conventions

- Go flat package — all files in `package main`, single directory
- No external frameworks — stdlib + minimal SDKs
- JSON everywhere — config, sessions. No YAML.
- Tool handlers: `func(json.RawMessage) (string, error)`
- Providers: `Provider` interface with `<-chan StreamChunk`
- File edits: search-and-replace (exact match, not line-number)
- After code changes: `make build` and `make install` (or `make release`)

## The .agent File

One file = one agent. Frontmatter (`---`) for config, body = system prompt. Supports shebang (`#!/usr/bin/env simpleagent`).

```
#!/usr/bin/env simpleagent
---
description: Proxmox VE infrastructure manager
deny: delete, chmod
provider: ollama
model: qwen2.5-coder:14b
url: http://192.168.1.100:11434
---

You are a Proxmox infrastructure manager...

# skill: VM Management
...
```

Header fields (all optional): `description`, `deny`, `allow`, `model`, `provider`, `url`.
No frontmatter = entire file is the prompt. Skills are just markdown sections. No `api_key` in .agent files — keys come from config or env.

Detection: first positional arg ending in `.agent` = agent file. Direct path, no search. Rest = inline prompt.

Priority: CLI flags > agent file > project config > user config > defaults.

`--new [file.agent] [description]` creates via guided conversation. `--edit file.agent` modifies via conversation. Both use specialized builder/editor system prompts and run in action mode.

## CLI

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | — | LLM provider |
| `--model` | `-m` | Model name |
| `--session` | — | Resume session by ID or name |
| `--resume` | — | Resume last session |
| `--sessions` | — | List all sessions |
| `--new` | — | Create new .agent file |
| `--edit` | — | Edit existing .agent file |
| `--version` | — | Print version |

Providers: anthropic, openai, openrouter, gemini, ollama, bedrock

## Slash Commands

`/plan` `/action` `/new` `/rename <name>` `/sessions` `/compact` `/model <name>` `/provider <name>` `/memory <text>` `/help` `/exit`

**Shift+Tab** toggles plan/action. **Ctrl+C** interrupts streaming.

## Files

```
main.go              Entry, CLI flags, .agent file detection
agent.go             Agent loop, modes, slash commands, system prompt
agentfile.go         .agent file parser, builder/editor prompts
types.go             Mode, Message, ToolCall, StreamChunk, Usage
config.go            JSON config, layered loading, agentDir resolution
session.go           Session persistence, index, picker
memory.go            AGENT.md load/append
provider.go          Provider interface + factory
provider_anthropic.go
provider_openai.go   Also openrouter and ollama
provider_gemini.go
provider_bedrock.go
tools.go             Registry, dispatch, deny/allow, plan-mode blocking
tool_fs.go           read_file write_file edit_file list_dir delete move copy file_info make_dir chmod
tool_exec.go         bash start_process write_stdin read_output kill_process list_processes
tool_search.go       grep find_files
tool_diff.go         diff patch
tool_user.go         ask_user
render.go            Markdown rendering + context line
input.go             Raw terminal input, Shift+Tab detection
```

20 files. 21 tools (10 fs + 6 exec + 2 search + 2 diff + 1 user).

## Runtime Directories

One package-level var set once in `main()`:

- `agentDir` (`CWD/.simpleagent/<agent-name>/`) — per-agent sessions + memory, always in CWD

agentDir: `CWD/.simpleagent/<basename of .agent file>/` or `CWD/.simpleagent/default/` when no agent file.

```
~/.simpleagent/
  config.json                    User-wide: API keys, default provider/model

./project/.simpleagent/          (in each working directory)
  config.json                    Project: override provider/model per repo
  proxmox.agent/
    AGENT.md                     Agent memory (/memory command)
    sessions/                    Conversation history
  default/                       When no .agent file specified
    AGENT.md
    sessions/
```

## Config

Layered config with deep merge. Each layer only overrides the fields it sets.

```
1. Hardcoded defaults
2. ~/.simpleagent/config.json      (user-wide — API keys, preferred provider/model)
3. .simpleagent/config.json        (project — override provider/model per repo)
4. .agent file frontmatter         (agent-specific — provider, model, url)
5. Environment variables           (override api_key/url)
6. CLI flags                       (highest priority — provider, model)
```

Provider-scoped config — each provider has `api_key`, `model`, `url`:

```json
{
  "provider": "anthropic",
  "providers": {
    "anthropic": {"api_key": "sk-ant-...", "model": "claude-sonnet-4-20250514"},
    "ollama": {"model": "qwen2.5-coder:14b", "url": "http://localhost:11434"}
  },
  "max_tokens": 8192,
  "bash_timeout": 120,
  "tools": {"deny": ["delete"], "allow": []}
}
```

Old flat config.json (with `anthropic_api_key`, `model` map, etc.) auto-migrates silently.
Tool policy in `.agent` file overrides config.json when present.

Env overrides: `ANTHROPIC_API_KEY` `OPENAI_API_KEY` `OPENROUTER_API_KEY` `GEMINI_API_KEY` `OLLAMA_HOST` `SIMPLEAGENT_MAX_TOKENS`

## Modes

| Mode | Tools | Behavior |
|------|-------|----------|
| **Plan** | Read-only | Gather info, ask questions, build a plan |
| **Action** | All | Autonomous execution |

New sessions → plan. Resumed → action. Write tools blocked at registry level.

## System Prompt

1. Persona (`.agent` file body or default)
2. Working dir + mode
3. Tools (filtered by policy)
4. Rules (ACT don't narrate)
5. Mode instructions
6. Agent memory (AGENT.md from agentDir)

## Versioning

`yymmddvv` — date + daily counter. `VERSION` file, embedded via `-ldflags "-X main.version=$(cat VERSION)"`.

## CI/CD

```
feature/* ──PR──> develop ──PR──> main
                     │              │
                  nightly        release
```

| Workflow | Trigger | Action |
|----------|---------|--------|
| ci.yml | PR to main/develop | vet + cross-compile |
| nightly.yml | Push to develop | Rolling `nightly` pre-release |
| release.yml | Push to main | Bump, changelog, tag, GitHub release |

Platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

## UI

Sequential stdout. No TUI. Works over SSH/serial/telnet. Minimal ANSI. Raw mode only for input.

## Doc Policy

CLAUDE.md is the single project reference. Update existing sections, never add new ones. This size is the cap.
