<div align="center">

# Gliss

**A terminal coding agent built from scratch with Go + Bubble Tea**

Streaming LLM chat · tool calling · TUI, all built in — no official SDKs

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/Bubble_Tea-TUI-FF69B4)](https://github.com/charmbracelet/bubbletea)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/Star-Rats/gliss/pulls)

English | [简体中文](README.md)

</div>

```
█▀▀▀ █ █ █▀▀ █▀▀    ◆ gliss
█▄▄▀ █ █ ▀▀█ ▀▀█    glm-4.7 ● ready
▀▄▄▀ ▀ ▀ ▀▀▀ ▀▀▀    ~/code/jmy/Gliss  ↑1.2k ↓800

❯ Add a Mul function to util.Add and run the tests
◆  Let me look at the existing code…
✓ read_file path=internal/util/util.go →    12 lines
✓ edit_file path=… old=<1 char> new=<23 chars>
✓ run_bash command="go test ./internal/util/" → …
◆  Done: added Mul and all 3 tests pass.
── turn end: 4 LLM calls, 3 tool calls ──
> Type a task, /help for commands, Enter to send…
```

## ✨ Highlights

- 🔌 **Any OpenAI-compatible endpoint**: Zhipu GLM / DeepSeek / Kimi / OpenAI — switch with one env var, or hot-swap mid-session with `/model`
- 📡 **Hand-written SSE streaming**: accumulates `tool_calls` deltas by index; zero official SDK dependencies — one `main.go` plus five `internal/` packages is the whole codebase
- 🛠️ **7 built-in tools**: read/write files, precise editing, glob/grep search, shell execution — with **path-escape protection** (resolved absolute paths must stay inside the working directory) and command timeouts
- ⏸️ **Interruptible anytime**: `Esc` cancels through `context.WithCancel`, cutting both the LLM HTTP request and the running bash subprocess
- 🎛️ **Per-turn call cap** (`-max-turns`, default 40): guards against runaway tool loops burning tokens
- 🖥️ **Built for the terminal**: gradient ASCII banner, scrollable history, multi-line input, slash-command palette (`↑↓` select / `tab` complete), `ctrl+t` to expand tool-call details, Markdown rendered with glamour, token usage tracking

## 🚀 Quick Start

```bash
# Build (Go 1.22+)
go build -o gliss .

# Point it at any OpenAI-compatible endpoint
export GLISS_API_KEY=sk-xxx
export GLISS_BASE_URL=https://api.deepseek.com/v1
export GLISS_MODEL=deepseek-chat

./gliss
```

Common endpoints:

| Provider | GLISS_BASE_URL | Example model |
|---|---|---|
| Zhipu GLM | `https://open.bigmodel.cn/api/paas/v4` | `glm-4.7` |
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-chat` |
| Moonshot Kimi | `https://api.moonshot.cn/v1` | `kimi-k2-0905-preview` |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o` |

## ⚙️ Configuration

Priority: CLI flags > environment variables > config file > defaults.

```bash
gliss -base-url https://api.deepseek.com/v1 -model deepseek-chat -api-key sk-xxx
```

| Env var | Flag | Description |
|---|---|---|
| `GLISS_API_KEY` / `OPENAI_API_KEY` | `-api-key` | API key (required) |
| `GLISS_BASE_URL` / `OPENAI_BASE_URL` | `-base-url` | API base URL, default `https://api.openai.com/v1` |
| `GLISS_MODEL` | `-model` | Model name, default `gpt-4o` |
| | `-max-turns` | Max LLM calls per turn (guards tool loops), default 40 |

Config file at `~/.config/gliss/config.json` (override path with `$GLISS_CONFIG`):

```json
{
  "base_url": "https://open.bigmodel.cn/api/paas/v4",
  "api_key": "sk-xxx",
  "model": "glm-4.7",
  "max_turns": 40,
  "bash_timeout_seconds": 120
}
```

## ⌨️ Keybindings

| Key | Action |
|---|---|
| `Enter` | Send input |
| `Shift+Enter` | New line inside the input box |
| `Esc` | Interrupt the running turn; clears the input when idle |
| `ctrl+t` | Toggle details of all tool calls (args & results) |
| `PgUp` / `PgDn` (`Home` / `End`) | Scroll history |
| `ctrl+c` | Quit |

## ⌘ Slash Commands

Type `/` in the input box to open the command palette: keep typing to filter, `↑↓` to select, `tab` to complete, `enter` to run, `esc` to close.

| Command | Action |
|---|---|
| `/help` | Show commands and keybindings |
| `/clear` (`/new`) | Clear session history and usage stats, start a new session |
| `/model [name]` | Show current model, or switch at runtime (e.g. `/model glm-4.7`) |
| `/usage` | Show cumulative token usage and call counts |
| `/tools` | List all built-in tools |
| `/exit` (`/quit`) | Quit |

Unknown `/`-prefixed input (e.g. a question about `/etc/hosts`) is sent to the model as a normal message.

## 🛠️ Built-in Tools

| Tool | Description |
|---|---|
| `read_file` | Read a file with line numbers; supports `offset` / `limit` |
| `write_file` | Overwrite a file; creates parent directories |
| `edit_file` | Exact string replacement; errors on non-unique matches, supports `replace_all` |
| `list_dir` | List a directory |
| `glob` | Filename matching with recursive `**` |
| `grep` | Regex content search, output as `file:line:text`, skips `.git` / binaries |
| `run_bash` | Run a shell command (120s timeout by default, output truncated at 30KB) |

Safety: every file path is resolved to an absolute path and must stay inside the working directory (`..` escapes are rejected); `run_bash` enforces a timeout.

## 🏗️ Architecture

```
main.go                 entry: config load → wiring → tea.Program
internal/config         configuration (flags > env > JSON file)
internal/llm            OpenAI-compatible client: /chat/completions + SSE parsing,
                        accumulating tool_calls deltas by index
internal/tools          tool interface + 7 implementations + registry (path-escape guard)
internal/agent          session loop goroutine: prompt → LLM stream → tool exec → backfill → loop;
                        emits events to the UI (Token/ToolStart/ToolEnd/TurnEnd/…)
internal/ui             Bubble Tea UI: gradient ASCII banner / scrollable history / multi-line input /
                        slash commands / keybindings; waitForAgentEvent Cmd pumps events,
                        streams tokens live, renders markdown with glamour when done
```

Concurrency: the agent runs in its own goroutine — `Submit()` feeds input, `Events()` streams output; the UI bridges them with Bubble Tea's standard Cmd event pump. `Esc` interruption flows through `context.WithCancel` into both the LLM HTTP request and the bash subprocess.

## ✅ Testing

```bash
go test ./...        # SSE parsing, toolset, agent loop, UI smoke tests (httptest mock server)
go test -race ./...
```

## License

[MIT](LICENSE)
