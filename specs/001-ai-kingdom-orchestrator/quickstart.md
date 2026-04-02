# Quickstart: Claude King

## Prerequisites

- Go 1.22+
- macOS or Linux
- Claude Code (or any MCP-aware AI agent)

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
