# Quickstart: Claude King

## Prerequisites

- Go 1.25+
- macOS or Linux
- Claude Code (or any MCP-aware AI agent)
- Optional: `codex` CLI (OpenAI) and/or `gemini` CLI (Google) if using those vassal types

## Build

```bash
# Clone and build
cd claude-king
go build -o king ./cmd/king
go build -o kingctl ./cmd/kingctl

# Install to PATH
sudo mv king kingctl /usr/local/bin/
```

## First Kingdom

```bash
# Navigate to your project
cd ~/my-project

# Launch a Kingdom (creates .king/ with default config if none exists)
king up

# Check status
kingctl status

# Output:
# Kingdom: my-project (running)
# Vassals:
#   shell  running  pid:12345  idle 5s ago
```

## Configure Vassals

Create `.king/kingdom.yml`:

```yaml
name: my-project

vassals:
  - name: api
    command: go run ./cmd/server
    autostart: true
    env:
      PORT: "8080"

  - name: monitor
    command: tail -f /var/log/app.log
    autostart: true

patterns:
  - name: errors
    regex: "ERROR|panic:"
    severity: error
```

Restart the Kingdom:

```bash
king down && king up
```

## Use from Claude Code

Once the King daemon is running, Scepter Tools are available in Claude Code:

```
> List all my terminal sessions
# Claude calls list_vassals() → shows api, monitor

> Run tests in the api terminal
# Claude calls exec_in("api", "go test ./...")

> What errors have happened recently?
# Claude calls get_events(severity: "error")
```

## AI Vassals (Claude, Codex, Gemini)

King can manage AI CLI tools as vassals alongside shell processes. Declare them in `.king/kingdom.yml`:

```yaml
vassals:
  - name: coder
    type: claude                        # Claude Code (default)
    repo_path: ./services/api
    specialization: "Go, REST APIs"     # shown in list_vassals for routing

  - name: frontend
    type: codex                         # OpenAI Codex CLI
    repo_path: ./services/web
    model: o4-mini
    specialization: "TypeScript, React"

  - name: analyst
    type: gemini                        # Google Gemini CLI
    repo_path: ./services/data
    model: gemini-2.0-flash
    specialization: "data analysis, SQL"
```

King launches a `king-vassal` subprocess for each AI vassal. When the sovereign calls `list_vassals()`, it gets `type` and `specialization` per vassal — enough to route `dispatch_task` to the right AI.

## CLI Quick Reference

```bash
king up              # Start Kingdom in current directory
king down            # Stop Kingdom and all vassals
kingctl status       # Show Kingdom and vassal status
kingctl list         # List vassals
kingctl exec <name> <cmd>  # Run command in vassal
kingctl logs <name>  # Show recent vassal output
kingctl events       # Show recent events
```
