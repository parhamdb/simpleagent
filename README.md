# simpleagent

Single Go binary LLM agent. No frameworks. One `.agent` file = one agent. Portable, shareable, self-contained.

## Install

Download a binary from [Releases](https://github.com/parhamdb/simpleagent/releases), or build from source:

```bash
git clone https://github.com/parhamdb/simpleagent.git
cd simpleagent
make build && make install
```

Requires Go 1.25+.

## Quick Start

```bash
simpleagent                          # Interactive, default agent
simpleagent "fix the login bug"      # One-shot task
simpleagent coder.agent              # Run an agent file
simpleagent coder.agent "fix bug"    # Agent file + one-shot
```

First run triggers the setup wizard to configure your provider and API key. Or run `simpleagent --setup` anytime.

## The .agent File

One file = one agent. Frontmatter for config, body = system prompt. Supports shebang for direct execution.

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

All header fields are optional. Skills are just markdown sections. No `api_key` in agent files -- keys come from config or environment.

### Create and Edit

```bash
simpleagent --new proxmox.agent              # Guided conversation to build an agent
simpleagent --new proxmox.agent "manage k8s" # Start with a description
simpleagent --edit proxmox.agent             # Iteratively edit an existing agent
chmod +x proxmox.agent && ./proxmox.agent    # Shebang execution
```

## Providers

| Provider | Env Variable | Notes |
|----------|-------------|-------|
| Anthropic | `ANTHROPIC_API_KEY` | Default provider |
| OpenAI | `OPENAI_API_KEY` | |
| OpenRouter | `OPENROUTER_API_KEY` | |
| Gemini | `GEMINI_API_KEY` | |
| Ollama | `OLLAMA_HOST` | Local, no API key needed |
| Bedrock | AWS credentials | Uses AWS SDK credential chain |

## Config

Layered config with deep merge -- each layer only overrides the fields it sets:

```
1. Hardcoded defaults
2. ~/.simpleagent/config.json       (user-wide)
3. .simpleagent/config.json         (project-level)
4. .agent file frontmatter          (agent-specific)
5. Environment variables
6. CLI flags                        (highest priority)
```

```json
{
  "provider": "anthropic",
  "providers": {
    "anthropic": {"api_key": "sk-ant-...", "model": "claude-sonnet-4-20250514"},
    "ollama": {"model": "qwen2.5-coder:14b", "url": "http://localhost:11434"}
  },
  "max_tokens": 8192,
  "bash_timeout": 120
}
```

## Modes

| Mode | Tools | Behavior |
|------|-------|----------|
| **Plan** | Read-only | Gather info, ask questions, build a plan |
| **Action** | All | Autonomous execution |

New sessions start in plan mode. Use **Shift+Tab** to toggle, or `/plan` and `/action`.

## CLI Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | | LLM provider |
| `--model` | `-m` | Model name |
| `--session` | | Resume session by ID or name |
| `--resume` | | Resume last session |
| `--sessions` | | List all sessions |
| `--new` | | Create new .agent file |
| `--edit` | | Edit existing .agent file |
| `--setup` | | Run setup wizard |
| `--version` | | Print version |

## Slash Commands

| Command | Description |
|---------|-------------|
| `/plan` | Switch to plan mode |
| `/action` | Switch to action mode |
| `/new` | Start a new session |
| `/rename <name>` | Name the current session |
| `/sessions` | List all sessions |
| `/compact` | Compress conversation history |
| `/model <name>` | Switch model |
| `/provider <name>` | Switch provider |
| `/memory <text>` | Save a note to agent memory |
| `/help` | Show help |
| `/exit` | Quit |

## Tools

21 built-in tools across 5 categories:

- **Files**: `read_file` `write_file` `edit_file` `list_dir` `delete` `move` `copy` `file_info` `make_dir` `chmod`
- **Exec**: `bash` `start_process` `write_stdin` `read_output` `kill_process` `list_processes`
- **Search**: `grep` `find_files`
- **Diff**: `diff` `patch`
- **User**: `ask_user`

Tool access can be restricted per-agent via `deny`/`allow` in the agent file or config.

## Runtime Directories

```
~/.simpleagent/
  config.json                      User-wide config

./project/.simpleagent/            Per working directory
  config.json                      Project-level config
  proxmox.agent/
    AGENT.md                       Agent memory (/memory command)
    sessions/                      Conversation history
  default/
    AGENT.md
    sessions/
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| Shift+Tab | Toggle plan/action mode |
| Ctrl+C | Interrupt streaming or exit |
| Ctrl+D | Exit |

## Build

```bash
make build       # Build binary
make install     # Install to GOPATH
make release     # Bump version + build + install
make vet         # Run go vet
make tidy        # Run go mod tidy
make clean       # Remove binary
```

Versioning: `yymmddvv` (date + daily counter), stored in [`VERSION`](VERSION).

## CI/CD

```
feature/* ──PR──> develop ──PR──> main
                     │              │
                  nightly        release
```

| Workflow | Trigger | Action |
|----------|---------|--------|
| [ci.yml](.github/workflows/ci.yml) | PR to main/develop | Vet + cross-compile |
| [nightly.yml](.github/workflows/nightly.yml) | Push to develop | Rolling `nightly` pre-release |
| [release.yml](.github/workflows/release.yml) | Push to main | Bump, changelog, tag, GitHub release |

Platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

## Documentation

- [CHANGELOG.md](CHANGELOG.md) -- Release history
- [CLAUDE.md](CLAUDE.md) -- Project reference and conventions
