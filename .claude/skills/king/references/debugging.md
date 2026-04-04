# King Debugging Guide

## Quick Diagnostics

```bash
# Is the daemon running?
ps aux | grep "king" | grep -v grep
ls .king/king-*.sock        # socket exists = daemon running
cat .king/king-*.pid        # PID file

# Daemon logs (most useful)
tail -50 .king/daemon.log

# Kingdom status
king status
king list
```

## Common Errors

### `king-vassal "X" did not start within 3s`
**Cause:** king-vassal subprocess hangs on startup.
**Classic cause:** Deadlock — king-vassal tries to `vassal.register` RPC to daemon before daemon's accept loop starts.
**Fix:** Already fixed in current codebase (registration is async goroutine).
**Check:** Run king-vassal manually with same args and see if it starts fast.

### `No Kingdom found. Run king up first.`
**Cause:** `king-vassal --stdio` can't find daemon socket via auto-discovery.
**Fix:** Start daemon first: `king up --detach`, then restart Claude Code MCP servers.

### `kingdom already running` (when running `king mcp`)
**Cause:** Daemon already running; `king mcp` tried to start a second daemon.
**Fix:** Already fixed — `king mcp` now attaches to existing daemon state.
**Check:** This error should no longer appear; if it does, `king mcp` attach code failed.

### `validate config: vassal "X": command must not be empty`
**Cause:** Old validator required `command` for all vassal types.
**Fix:** Already fixed — `type:claude` and `type:serial` don't require `command`.

### `listen unix .king/king-*.sock: bind: address already in use`
**Cause:** Stale socket file from crashed daemon.
**Fix:** `king down` or manually: `rm .king/king-*.sock .king/king-*.pid`

### `vassal_client_pool dial: connection refused`
**Cause:** king-vassal process died; its socket is stale.
**Fix:**
```bash
king down && king up --detach     # restart daemon (relaunches vassals)
# OR
rm .king/vassals/<name>.sock      # remove stale socket; daemon will retry
```

### Tool call to vassal times out after ~60s
**Cause:** VassalClient has a 60-second default read deadline when no explicit context deadline is set. If a vassal is stuck or unresponsive, tool calls (e.g. `exec_in`, `dispatch_task`) will fail after 60s.
**Fix:** The vassal process is likely hung. Restart it:
```bash
king down && king up --detach
```
**Note:** This timeout is intentional — it prevents indefinite hangs from blocking the entire daemon. If your workload legitimately needs longer, pass an explicit `timeout_seconds` parameter to `exec_in`.

### MCP server shows `failed` in Claude Code
1. Check if daemon is running
2. Check daemon log for errors
3. Restart MCP: `/mcp` → select server → Restart
4. For `king-vassal --stdio`: daemon must be running first

## Daemon Log Patterns

| Log message | Meaning |
|-------------|---------|
| `config loaded vassals=N` | Config read OK |
| `wrote pid file` | Daemon starting |
| `claude vassal started name=X` | Vassal subprocess launched OK |
| `vassal MCP server started` | king-vassal socket ready |
| `kingdom status changed status=running` | Full startup complete |
| `external vassal registered` | Vassal self-registered via RPC |
| `vassal exited, restarting` | Auto-restart triggered |
| `delegation warden: vassal X expired` | Delegated vassal timed out |

## Debug king-vassal Manually

```bash
# Test with exact daemon args
/path/to/king-vassal \
  --name firmware \
  --repo emwirs-esp32-firmware \
  --king-dir /path/to/.king \
  --king-sock /path/to/.king/king-<hash>.sock \
  --timeout 10
# Should print: INFO "vassal MCP server started" socket=...
```

## Socket Paths

```
.king/king-<8hex>.sock    ← daemon RPC socket (hash of rootDir path)
.king/king-<8hex>.pid     ← daemon PID file
.king/vassals/<name>.sock ← per-vassal MCP socket
```

## SQLite DB

```bash
# Inspect state directly
sqlite3 .king/king.db ".tables"
sqlite3 .king/king.db "SELECT name, status FROM kingdoms;"
sqlite3 .king/king.db "SELECT name, status FROM vassals WHERE kingdom_id='...';"
sqlite3 .king/king.db "SELECT * FROM events ORDER BY created_at DESC LIMIT 10;"
```
