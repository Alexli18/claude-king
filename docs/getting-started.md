# Getting Started with Claude King

> Practical onboarding guide for developers and power users working with multi-repo / multi-agent workflows.

---

## 1. Positioning — When King Is Useful (and When It's Not)

**Claude King is an orchestration control plane for running multiple Claude Code agents across sub-repositories from a single workspace.**

When you work across several repos at once — firmware, backend, docs, infra — you inevitably end up with 3–5 terminal windows, each running its own Claude Code session. You hold the state in your head, copy context between windows manually, and spend real time remembering which agent did what and where. This is coordination overhead, and it compounds as the number of repos grows. King exists to replace that multi-agent chaos with a formal system: a daemon, a shared task model, structured events, and a central point of control.

King is not a model and not a replacement for Claude Code. Every vassal agent is still Claude Code, running in its own repository context. King adds the orchestration layer on top — task dispatch, status tracking, event logs, cross-repo reads, and recovery — without changing how agents work internally.

King is not for everyone. If you work in a single repo with one Claude Code session, King adds complexity without benefit. Use it when the coordination problem is real.

| Use King when… | Don't use King when… |
| --- | --- |
| You regularly juggle multiple repo-local Claude sessions | You work in a single repo with one Claude Code session |
| You keep task state in your head across repos | Your project fits in one context window |
| You copy context between repos manually | You don't need structured task tracking or audit |
| You need to know what each agent is doing at a glance | You prefer a simpler, direct CLI workflow |
| You want task dispatch, status polling, and event history | You're evaluating King for a weekend project |
| You need graceful recovery after crashes or restarts | You do not need recovery or state continuity after restarts |

A common setup: a `firmware` vassal building embedded code, a `backend` vassal running API tests, and a `docs` vassal updating documentation — all dispatched in parallel from one sovereign workspace. King also fits hardware and robotics projects where multiple agents span device-side and software-side contexts.

**If coordination overhead across repos is your bottleneck, King is the right tool.**

## 2. Core Concepts

King runs a background daemon that manages a project-wide **kingdom**. You act as the **sovereign** — the human operator in the root workspace, working through Claude Code. You tell Claude Code what you need in natural language; Claude Code calls King's tools on your behalf. Repo-local Claude Code agents act as **vassals**, each working within its own sub-repository context.

King exposes its tools via [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) — the standard that lets Claude Code discover and call external tools. When you add King as an MCP server, Claude Code gains access to tools like `list_vassals`, `dispatch_task`, and `get_events`. You don't need to know MCP internals; just know that it's how the sovereign talks to the daemon.

King uses deliberate terminology. Learning the terms early makes the rest of the workflow straightforward.

```text
                        ┌──────────────────────┐
                        │     You (Sovereign)   │
                        │   root Claude Code    │
                        └──────────┬───────────┘
                                   │ MCP tools
                        ┌──────────▼───────────┐
                        │     King Daemon       │
                        │  orchestration layer  │
                        └──┬────────┬────────┬──┘
                           │        │        │
                    ┌──────▼──┐ ┌───▼────┐ ┌─▼───────┐
                    │ Vassal A│ │Vassal B│ │Vassal C │
                    │firmware │ │backend │ │  docs   │
                    └─────────┘ └────────┘ └─────────┘
```

### Core Entities

**King** — the orchestration system. The **King daemon** is the background process that manages vassals, dispatches tasks, collects events, and handles recovery. The **King MCP server** is the interface that exposes tools to the sovereign inside Claude Code.

**Kingdom** — the orchestration boundary for a project. A kingdom defines the scope of coordination: which vassals exist, how they're configured, and where state lives. On disk, a kingdom is rooted at a `.king/` directory containing configuration, task metadata, and daemon state. Think of it as the project-level coordination perimeter, not just a folder.

**Sovereign** — the human operator. This is you, working in the root workspace through Claude Code. You decide what to dispatch, when to check status, and whether to approve actions. Claude Code is your interface — it translates your natural language into King tool calls. There is one sovereign per kingdom.

**Vassal** — a repo-local Claude Code agent managed by the King daemon. Each vassal is a separate Claude Code-driven agent process attached to its own sub-repo context, executing work dispatched by the sovereign.

**Task** — a unit of work dispatched to a vassal. Tasks move through states: `running`, `completed`, or `failed`. The sovereign dispatches tasks, polls their status, and can abort them if needed. Task state is persisted, so recovery is possible after restarts.

### Operational Concepts

**Events** — the live operational feed of what's happening across the kingdom. Use events to monitor agent activity, catch warnings, and track progress in real time.

**Audit log** — the detailed historical record of actions taken within the kingdom. While events show what's happening now, the audit log provides the full trace of what happened and when.

**Artifacts** — registered outputs from vassal work, such as files, test results, or build outputs. Artifacts can be registered by name and resolved later from any context within the kingdom.

**Approvals** — explicit sovereign sign-off required before certain operations proceed. King can gate sensitive actions behind an approval step, giving the operator control over what vassals are allowed to do.

**Guards** — automated health watchers that monitor vassal behavior and can block delegation when thresholds are breached. See [Next Steps](#8-next-steps) for more.

---

Everything in the quickstart and workflow sections below maps directly to these concepts.

## 3. Prerequisites & Installation

### Supported Platforms

| Platform | Status |
| --- | --- |
| macOS (arm64, amd64) | Supported |
| Linux (amd64, arm64) | Supported |
| Windows | Not supported (requires Unix PTY) |
| Windows (WSL) | May work, but is not officially tested |

### Prerequisites

- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) installed and working
- A project split across multiple repos or clearly separated sub-project contexts

### Recommended: Installer Script

```bash
curl -fsSL https://raw.githubusercontent.com/alexli18/claude-king/master/install.sh | bash
```

### Alternative: Build from Source

Requires Go 1.25+.

```bash
git clone https://github.com/alexli18/claude-king && cd claude-king
make build && make install-user
```

This installs `king` and `king-vassal` binaries into your local path. `king` is the main CLI you interact with; `king-vassal` is the agent process that the daemon launches automatically for each vassal — you don't need to run it directly.

## 4. Quickstart: First 5 Minutes

### Step 1. Start the Daemon

Navigate to your project root and start King in the background:

```bash
cd ~/your-project
king up --detach
```

King creates a `.king/` directory and starts the daemon. Logs go to `.king/daemon.log`.

### Step 2. Verify the Kingdom Is Alive

```bash
king status   # check the current kingdom
king list     # show all running kingdoms on this machine
```

Both commands should show your kingdom as running.

### Step 3. Add King as an MCP Server

Create or update `.mcp.json` in your project root:

```json
{
  "mcpServers": {
    "king": {
      "command": "king",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Code. After restart, Claude Code can see and call King's tools.

### Step 4. Make Your First Tool Call

Ask Claude Code to verify the connection. For example:

> "Show me which vassals are currently running."

This is the smoke test. Claude Code calls `list_vassals` behind the scenes. At this point you haven't configured any vassals yet, so you'll see an empty list — that's expected. The point is to confirm that the MCP connection works: if the call succeeds without errors, the sovereign can talk to the daemon.

Follow up with:

> "Summarize any recent warnings or errors from the kingdom."

Claude Code calls `get_events` and returns the results. If both calls succeed, your kingdom is live and the sovereign is connected. You'll add vassals and dispatch real work in the next section.

## 5. First Real Workflow

In the quickstart, the goal was to prove that the kingdom and MCP connection work. For a real workflow, you need at least one explicitly configured vassal.

### Add a Vassal

Create `.king/kingdom.yml` with one vassal definition:

```yaml
name: my-project
vassals:
  - name: api
    type: claude
    repo_path: ./services/api
```

Restart the daemon to pick up the config:

```bash
king down && king up --detach
```

Verify the vassal is visible:

> "Show me which vassals are currently running."

You should see `api` listed as a running vassal.

### Dispatch a Task

Ask the sovereign to send work to the vassal:

> "Dispatch a task to api: run all unit tests and summarize any failures."

Under the hood, Claude Code calls `dispatch_task(vassal="api", task="run all unit tests and summarize any failures")`. King assigns a `task_id` and the vassal begins working.

### Poll Task Status

The task runs asynchronously. Check on it:

> "What's the status of the task I just dispatched to api?"

Claude Code calls `get_task_status` and returns something like:

```text
Task abc123 — status: running
```

Ask again after a moment:

```text
Task abc123 — status: completed
Result: 14 tests passed, 2 failed.
  FAIL TestUserCreate — expected 201, got 400
  FAIL TestOrderValidation — nil pointer on empty cart
```

If the task fails or hangs, you can abort it:

> "Abort the task on api."

### Check Events

After the task completes, review what happened:

> "Show me the last 10 kingdom events."

Events give you the live operational timeline: when the task was dispatched, when the vassal started working, and what the outcome was.

### A Note on dispatch_task vs exec_in

In this workflow we used `dispatch_task` — this sends work to the vassal as an AI agent that thinks and acts. There's also `exec_in`, which runs a specific shell command directly in the vassal's PTY (pseudo-terminal), like `go test ./...` or `make build`. Note: `exec_in` works for running direct commands, but if you try to use it on a Claude-type vassal for open-ended work (where `dispatch_task` is the right tool), the system returns a helpful error guiding you to use `dispatch_task` instead.

For a detailed comparison with examples, see "dispatch_task vs exec_in — Quick Reference" in Common Workflows below.

## 6. Common Workflows

### Parallel Dispatch Across Multiple Repos

**When you need this:** You have several sub-repos and want multiple vassals working at the same time.

**Setup:** Define multiple vassals in `.king/kingdom.yml`:

```yaml
name: my-project
vassals:
  - name: api
    type: claude
    repo_path: ./services/api
  - name: frontend
    type: claude
    repo_path: ./services/frontend
  - name: docs
    type: claude
    repo_path: ./docs
```

**What to do:** Dispatch tasks to each vassal in sequence. Once dispatched, the tasks run concurrently across multiple vassals.

> "Dispatch to api: run all unit tests and report failures."
>
> "Dispatch to frontend: check for TypeScript errors and list them."
>
> "Dispatch to docs: verify all internal links are valid."

These are three independent tasks. The daemon tracks them concurrently from the same sovereign session. Poll each task or read events to track progress:

> "Show me the status of all running tasks."

**Why it helps:** Instead of managing three separate Claude Code sessions and switching between terminals, the sovereign coordinates all work from one place. Tasks run concurrently, and results come back to the same context.

---

### Cross-Repo Reads with read_neighbor

**When you need this:** One vassal needs to see a file from another repo without leaving its own context.

**What to do:** Use `read_neighbor` to read files across repo boundaries.

> "Read `services/api/handlers/orders.go` so I can verify the backend response contract before updating the frontend."

Under the hood, Claude Code calls `read_neighbor(path="services/api/handlers/orders.go")` and returns the file contents. No manual copy-paste, no context switching.

**Why it helps:** In polyrepo setups, shared contracts, config files, and schemas often live in a different repo than where you're working. `read_neighbor` lets the sovereign pull that context without leaving the current workflow.

---

### dispatch_task vs exec_in — Quick Reference

**When you need this:** You're unsure whether to dispatch a task or run a command directly.

| Situation | Use | Example |
| --- | --- | --- |
| You want the vassal to think, investigate, or solve a problem | `dispatch_task` | "Find why the auth tests are failing and suggest a fix" |
| You want to run a specific command and see the output | `exec_in` | "Run `make build` in firmware" |
| You want a test summary with analysis | `dispatch_task` | "Run tests and explain what's broken" |
| You want raw test output | `exec_in` | "Run `go test ./...` in api" |

**Rule of thumb:** If you'd ask a colleague to "figure this out," use `dispatch_task`. If you'd type the command yourself, use `exec_in`.

---

### Basic Recovery

**When you need this:** Something went wrong — a vassal stopped responding, the daemon crashed, or state looks stale.

**Step 1. Check what's happening:**

```bash
king status
tail -20 .king/daemon.log
```

**Step 2. Force stop and restart:**

```bash
king down --force
king up --detach
```

`king down --force` cleans up zombie processes and stale state. In-flight tasks are interrupted, and King attempts to recover persisted task state after restart.

**Step 3. Verify recovery:**

```bash
king status
king list
```

Then reconnect from Claude Code:

> "Call list_vassals and confirm everything is back."

**What the system handles for you:** On relaunch, King cleans up stale state and attempts to restore task state and vassal connectivity automatically.

## 7. Troubleshooting

If something doesn't behave as expected, start here:

```bash
king status
king list
tail -50 .king/daemon.log
```

These three commands cover most diagnostic needs. `daemon.log` is the primary source of detail for startup and lifecycle issues.

---

### King Is Not Visible in Claude Code

**Symptom:** MCP tools like `list_vassals` or `get_events` don't appear. The sovereign can't interact with King.

**Likely cause:** The `.mcp.json` file is missing, malformed, or Claude Code wasn't restarted after adding it.

**What to try:**

1. Verify `.mcp.json` exists in your project root with the correct entry:

   ```json
   {
     "mcpServers": {
       "king": {
         "command": "king",
         "args": ["mcp"]
       }
     }
   }
   ```

2. Restart Claude Code.
3. Confirm the daemon is running: `king status`.

---

### The Daemon Does Not Start or Exits Immediately

**Symptom:** `king up --detach` completes but `king status` shows nothing, or the daemon exits right away.

**Likely cause:** An installation problem, a path or configuration issue, or invalid kingdom configuration.

**What to try:**

1. Check the log: `tail -50 .king/daemon.log`.
2. Verify the `king` binary is in your PATH: `which king`.
3. If you have a `kingdom.yml`, check it for syntax errors.
4. Try a clean start: `king down --force && king up --detach`.

---

### I Used exec_in, but This Should Have Been a Task

**Symptom:** You asked the sovereign to run something in a vassal using `exec_in`, but got a helpful error saying "use dispatch_task instead," or the result was raw output when you wanted analysis.

**Likely cause:** `exec_in` runs a shell command directly in the vassal's PTY. It doesn't invoke the AI agent. For work that requires thinking — investigating bugs, summarizing test failures, proposing changes — you need `dispatch_task`.

**What to try:**

- Use `dispatch_task` for agentic work: "Dispatch to api: find why auth tests fail and suggest a fix."
- Use `exec_in` for direct commands: "Run `go test ./...` in api."
- See the [dispatch_task vs exec_in table](#dispatch_task-vs-exec_in--quick-reference) in Common Workflows.

---

### Configuration and Path Confusion

**Symptom:** Vassals don't appear, the daemon can't find the config, or paths seem wrong.

**Likely cause:** The directory layout doesn't match what King expects. The most common mistakes:

- Running `king up` from the wrong directory (not the kingdom root).
- Placing `kingdom.yml` in the project root instead of `.king/kingdom.yml`.
- Setting `repo_path` in `kingdom.yml` to an incorrect relative path.
- Placing `.mcp.json` in a subdirectory instead of the project root.

**What to try:**

1. Confirm your kingdom root: this is the directory where `.king/` lives.
2. Run `king up --detach` from the kingdom root.
3. Check that `.king/kingdom.yml` exists (not `./kingdom.yml`).
4. Verify each vassal's `repo_path` resolves correctly relative to the kingdom root.
5. Confirm `.mcp.json` is in the same directory where you open Claude Code.

---

### The Kingdom Seems Stuck After Restart or Failed Shutdown

**Symptom:** After a crash, forced quit, or restart, vassals don't reconnect. State looks stale. Commands hang or return unexpected results.

**Likely cause:** The previous daemon didn't shut down cleanly, leaving behind stale Unix sockets, zombie processes, or incomplete state.

**What to try:**

1. Force stop everything:

   ```bash
   king down --force
   ```

2. Restart cleanly:

   ```bash
   king up --detach
   ```

3. Verify:

   ```bash
   king status
   king list
   ```

**What the daemon handles automatically:** On relaunch, stale sockets are removed, orphaned tasks are recovered, and vassal connections are re-established cleanly. Health-related state resets to clean defaults, so a previous failure won't block the system after a fresh start.

## 8. Next Steps

You've covered the basics: starting a kingdom, dispatching tasks, reading events, and recovering from failures. Here's where to go deeper:

**Operations & Safety**

- **Guards & Health Monitoring** — Automatically watch vassal health (port checks, log patterns, data rates, health scripts) and block delegation when thresholds are breached.
- **Audit Log & Action Traces** — Full historical record of actions. Use `get_audit_log` and `get_action_trace` to inspect what happened and when.
- **Approvals** — Gate sensitive operations behind explicit sovereign sign-off with `respond_approval`.

**Outputs & Coordination**

- **Artifacts** — Register and resolve named outputs (files, build results, test reports) across vassal boundaries with `register_artifact` and `resolve_artifact`.

**Advanced Configuration**

- **Kingdom Configuration** — Per-vassal model selection, custom guard definitions, multi-kingdom setups, and serial/hardware event streams for embedded workflows.
- **Security Model** — See [Permission Model](permission-model.md) for details on access control and execution boundaries.

For the full MCP tool reference, see the [README](../README.md).
