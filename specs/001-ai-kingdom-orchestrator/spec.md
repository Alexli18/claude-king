# Feature Specification: Claude King — AI Kingdom Orchestrator

**Feature Branch**: `001-ai-kingdom-orchestrator`  
**Created**: 2026-04-02  
**Status**: Draft  
**Input**: User description: "Claude King — autonomous host orchestrator that turns scattered terminals and AI agent processes into a unified hierarchical ecosystem (Kingdom), where a central agent (King) coordinates specialized agents (Vassals) with full system, file, and hardware state awareness."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Launch a Kingdom Environment (Priority: P1)

As a power user working on a complex project with multiple terminal sessions, I want to navigate to my project directory and run a single command (`king up`) so that all required terminal sessions, ports, and vassal agents are initialized from my project's configuration, and the central King agent is ready to coordinate work across them.

**Why this priority**: This is the foundational capability — without the ability to stand up a Kingdom, no other features can function. It delivers immediate value by replacing manual terminal/tmux setup.

**Independent Test**: Can be fully tested by running `king up` in a configured project directory and verifying that PTY sessions are created, the daemon is running, and the MCP server is accessible. Delivers value by eliminating multi-step environment setup.

**Acceptance Scenarios**:

1. **Given** a project directory with a `.king` configuration, **When** the user runs `king up`, **Then** the King daemon starts, creates all configured PTY sessions, and reports readiness within 10 seconds.
2. **Given** a running Kingdom, **When** the user runs `king up` again in the same directory, **Then** the system detects the existing Kingdom and reconnects rather than creating a duplicate.
3. **Given** a project directory without `.king` configuration, **When** the user runs `king up`, **Then** the system initializes a default Kingdom with a single session and creates a minimal `.king` configuration.

---

### User Story 2 - Cross-Terminal Command Execution (Priority: P1)

As a developer using Claude Code in my main terminal, I want to execute commands in other terminal sessions (vassals) without leaving my current context, so that I can build firmware, run tests, or restart services in background terminals while staying focused on my primary work.

**Why this priority**: This is the core "zero-switching" value proposition. Users need to control vassals from the King session to eliminate context switching between terminal windows.

**Independent Test**: Can be tested by launching a Kingdom with 2+ sessions, then using MCP tools (`exec_in`, `list_vassals`) from Claude Code to run commands and read output in other sessions.

**Acceptance Scenarios**:

1. **Given** a running Kingdom with multiple vassals, **When** the user invokes `exec_in("vassal-name", "make build")` via Claude Code, **Then** the command executes in the target vassal's PTY and the output is returned to the King session.
2. **Given** a running Kingdom, **When** the user invokes `list_vassals()`, **Then** a list of all active vassal sessions is returned with their current status (idle, running command, error).
3. **Given** a command that produces continuous output (e.g., `tail -f`), **When** executed via `exec_in`, **Then** the system streams output incrementally rather than blocking until completion.

---

### User Story 3 - Cross-Terminal Event Awareness (Priority: P2)

As a developer working on a Go API in Claude Code while an ESP32 serial log runs in a background terminal, I want the King to automatically detect errors or significant events in any vassal's output and surface them in my active session, so that I can react to problems without monitoring every terminal.

**Why this priority**: This is the "Global Awareness" differentiator — automatically bridging context between isolated terminals. It builds on P1 capabilities and transforms passive terminals into active information sources.

**Independent Test**: Can be tested by configuring an event detection rule, generating a matching event in a vassal terminal, and verifying that a notification appears in the King session with relevant context.

**Acceptance Scenarios**:

1. **Given** a vassal terminal streaming logs, **When** an error pattern is detected in the output, **Then** the King session receives a contextual alert summarizing what happened and which vassal produced it.
2. **Given** multiple vassals producing high-volume output, **When** filtered through the semantic sieve, **Then** only significant events are surfaced to the King, reducing noise by at least 90%.
3. **Given** an event that correlates with recent changes in the King's active session (e.g., an ESP32 error matching a recently committed API change), **When** the alert is generated, **Then** it includes a suggested correlation to help the user diagnose the root cause.

---

### User Story 4 - File and Artifact Sharing Between Agents (Priority: P2)

As a user orchestrating multiple specialized agents, I want to share files and build artifacts between vassal sessions using short references (e.g., `king://artifacts/firmware.bin`), so that agents can consume each other's outputs without manual file path management.

**Why this priority**: Multi-agent workflows often produce intermediate artifacts that need to flow between agents. This feature eliminates manual path coordination and enables end-to-end automation pipelines.

**Independent Test**: Can be tested by registering an artifact from one vassal, then accessing it by reference from another vassal, verifying the file is correctly resolved and accessible.

**Acceptance Scenarios**:

1. **Given** a vassal that produces a build artifact, **When** the artifact is registered in the Artifact Ledger, **Then** other vassals can access it via the `king://artifacts/` reference scheme.
2. **Given** a registered artifact, **When** a vassal requests it by name, **Then** the system resolves the reference to the actual file path and provides access within 1 second.
3. **Given** an artifact that has been updated by its producer, **When** a consumer accesses it, **Then** the consumer receives the latest version.

---

### User Story 5 - Kingdom Lifecycle Management (Priority: P3)

As a user managing 10–50 projects, I want each project's Kingdom to be fully isolated (ports, sessions, state) and to be able to shut down, restart, or inspect any Kingdom independently, so that projects never interfere with each other and resources are properly released.

**Why this priority**: Essential for the multi-project use case, but depends on the core Kingdom launch and command features being stable first. Ensures long-term reliability for heavy users.

**Independent Test**: Can be tested by launching two Kingdoms in separate project directories, verifying they don't share state, then shutting one down and confirming the other is unaffected.

**Acceptance Scenarios**:

1. **Given** two project directories each with `.king` configuration, **When** both Kingdoms are running simultaneously, **Then** each uses isolated sockets, ports, and state with no cross-contamination.
2. **Given** a running Kingdom, **When** the user runs `king down`, **Then** all PTY sessions are terminated, sockets are cleaned up, and resources are released within 5 seconds.
3. **Given** a Kingdom that crashed unexpectedly, **When** the user runs `king up` again, **Then** the system detects stale state, cleans it up, and starts fresh.

---

### User Story 6 - Interactive Dashboard (Priority: P3)

As a user managing a complex Kingdom with many vassals, I want a terminal-based interactive dashboard showing the status of all sessions, system load, and recent events, so that I can get a visual overview of my entire environment at a glance.

**Why this priority**: While not essential for core functionality, the TUI dashboard dramatically improves the user experience for complex multi-vassal setups and serves as the "visual control center."

**Independent Test**: Can be tested by launching the TUI dashboard with a running Kingdom and verifying that vassal statuses, system metrics, and event logs are displayed and update in real time.

**Acceptance Scenarios**:

1. **Given** a running Kingdom, **When** the user invokes the TUI dashboard, **Then** an interactive terminal UI displays all vassal statuses, recent events, and system health metrics.
2. **Given** the dashboard is open, **When** a vassal's status changes (e.g., error, command completes), **Then** the dashboard updates within 2 seconds.
3. **Given** the dashboard is open, **When** the user selects a vassal, **Then** they can view its recent output and execute commands directly from the dashboard.

### Edge Cases

- What happens when the system runs out of PTY slots? The system should report a clear error and suggest closing unused sessions.
- How does the system handle a vassal process that hangs indefinitely? A configurable timeout should be available, with options to forcefully terminate after warning.
- What happens when the King daemon crashes while vassals are running? Vassals should continue running independently; restarting the King should reattach to existing sessions.
- How does the system handle concurrent `exec_in` calls to the same vassal? Commands should be queued and executed sequentially with clear ordering guarantees.
- What happens when the user's disk is full and the log/state database can't write? The system should degrade gracefully, continuing to operate without persistent logging and alerting the user.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide a daemon process that manages PTY sessions and exposes an MCP server for AI agent communication.
- **FR-002**: System MUST allow creation, listing, and termination of vassal (PTY) sessions through both MCP tools and CLI commands.
- **FR-003**: System MUST support executing arbitrary commands in any vassal session and returning the output to the requesting session.
- **FR-004**: System MUST provide real-time event detection by monitoring vassal output for configurable patterns (errors, warnings, custom patterns).
- **FR-005**: System MUST filter and compress high-volume vassal output into summarized events before surfacing them to the King, reducing token consumption by at least 90%.
- **FR-006**: System MUST maintain an Artifact Ledger for registering, versioning, and resolving file artifacts shared between vassals.
- **FR-007**: System MUST isolate each project's Kingdom at the directory level, ensuring no shared state between Kingdoms in different project directories.
- **FR-008**: System MUST persist Kingdom state (session configurations, event logs, artifact registry) to survive daemon restarts.
- **FR-009**: System MUST expose a CLI utility (`kingctl`) for quick daemon interaction without requiring an LLM.
- **FR-010**: System MUST support a vassal self-declaration protocol (`vassal.json`) for repositories to advertise their skills and artifacts.
- **FR-011**: System MUST provide an interactive TUI dashboard showing vassal statuses, system health, and event history.
- **FR-012**: System MUST allow reading files from neighboring project directories via `read_neighbor(path)` when explicitly permitted by the user.

### Key Entities

- **Kingdom**: A project-scoped orchestration environment. Contains the daemon, vassal sessions, configuration, and state. Rooted in a `.king` directory within a project folder.
- **King (Daemon)**: The central background process managing all PTY sessions, the MCP server, event processing, and artifact registry for a single Kingdom.
- **Vassal**: A managed PTY session representing a terminal context. Can be a development server, build process, serial monitor, log stream, or any other terminal-based process. Self-declares skills via `vassal.json`.
- **Artifact**: A file or build product registered in the Artifact Ledger. Addressed via `king://artifacts/` references. Has a producer vassal, version, and file path.
- **Event**: A significant occurrence detected in vassal output by the Semantic Sieve. Contains source vassal, timestamp, severity, summary, and optional correlation data.
- **Scepter Tools**: The set of MCP-exposed tools (`list_vassals`, `exec_in`, `read_neighbor`) injected into the AI agent's context for Kingdom interaction.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can launch a fully configured multi-terminal environment with a single command in under 10 seconds (Zero-Switching goal).
- **SC-002**: Users do not need to manually switch terminal windows for more than 90% of their cross-terminal interactions during a work session.
- **SC-003**: The AI agent receives at least 10x fewer noise tokens from background terminal output compared to raw log ingestion, while preserving 95% of actionable diagnostic information (Context Density goal).
- **SC-004**: A new complex environment with 5+ terminal sessions can be torn down and redeployed within 15 seconds (Deployment Speed goal).
- **SC-005**: Cross-terminal command execution (`exec_in`) returns output to the requesting session within 2 seconds for commands completing in under 1 second.
- **SC-006**: Event detection surfaces critical errors from vassal output within 5 seconds of occurrence.
- **SC-007**: Users managing 10+ simultaneous Kingdoms experience no cross-contamination of state or resources between projects.

## Assumptions

- Users have a Unix-like operating system (macOS or Linux) with PTY support. Windows is out of scope for v1.
- Users have Claude Code or a compatible MCP-aware AI agent installed and configured.
- Users are comfortable with terminal-based workflows and CLI tools.
- The system targets single-machine orchestration; distributed multi-machine Kingdom management is out of scope for v1.
- The TUI dashboard (Phase 4) is a polish feature and not required for MVP functionality.
- Vassal processes are long-running terminal programs (servers, monitors, build watchers) rather than one-shot scripts (which can use `exec_in` directly).
- The Semantic Sieve uses pattern matching and heuristic rules for v1; ML-based semantic filtering is a potential future enhancement.
- Standard file system permissions govern cross-project file access via `read_neighbor`.
