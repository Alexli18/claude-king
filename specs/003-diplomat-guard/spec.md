# Feature Specification: King v2.1 — The Diplomat & The Guard

**Feature Branch**: `003-diplomat-guard`
**Created**: 2026-04-03
**Status**: Draft
**Input**: User description: "King v2.1 — The Diplomat & The Guard — delegation control and sanity guards"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Multi-Agent Delegation (Priority: P1)

A developer is working simultaneously in two AI sessions: one for the backend and one for the frontend. The backend AI requests exclusive control over the `api` vassal. The King daemon grants this request and blocks the frontend AI from stopping or modifying the `api` vassal until control is released.

**Why this priority**: This is the core value proposition — preventing conflicting AI actions on shared processes. Without this, multiple AI agents can interfere with each other, causing unpredictable system state.

**Independent Test**: Can be fully tested by launching two AI sessions targeting the same vassal, verifying that only the first to delegate gets control, and the second is rejected with a clear message.

**Acceptance Scenarios**:

1. **Given** two AI sessions are active, **When** Session A requests control of vassal `api`, **Then** Session A becomes the exclusive controller and Session B's control requests are rejected.
2. **Given** Session A controls vassal `api`, **When** Session B attempts to stop vassal `api`, **Then** the action is blocked and Session B receives a message explaining which session holds control.
3. **Given** Session A holds control, **When** Session A explicitly releases control, **Then** any session (including Session B) can immediately request control.

---

### User Story 2 - Automatic Control Recovery (Priority: P2)

A developer closes their laptop mid-session. The AI session controlling a vassal disconnects without explicitly releasing control. The King daemon detects the disconnection and automatically reclaims control after 30 seconds, ensuring all vassals continue running normally without human intervention.

**Why this priority**: Ensures system resilience. Without automatic recovery, a crashed or closed AI session could permanently lock a vassal, blocking all future AI interactions.

**Independent Test**: Can be fully tested by manually killing an AI session that holds delegation, waiting 30 seconds, and verifying the daemon reclaims control and vassals remain operational.

**Acceptance Scenarios**:

1. **Given** an AI session holds control of a vassal, **When** the session closes unexpectedly (crash/network drop), **Then** the daemon detects the loss of heartbeat within 30 seconds and automatically reclaims control.
2. **Given** control has been reclaimed after a disconnect, **When** a new AI session starts, **Then** it can immediately request and receive delegation without manual intervention.
3. **Given** the daemon reclaimed control after a disconnect, **When** the user checks system status, **Then** all vassals show as running and under daemon control.

---

### User Story 3 - Hallucination Block via Guard (Priority: P2)

An AI session reports that it has successfully fixed a firmware bug on an ESP32 device. Before applying any changes, the King daemon runs configured guard checks. The guard detects that data from the serial port is corrupted or below the expected rate. The daemon blocks the commit and displays a clear rejection message to the user.

**Why this priority**: Directly prevents the core problem of AI "hallucinations" causing real-world damage. Protects physical systems from incorrect AI claims.

**Independent Test**: Can be fully tested by configuring a `data_rate` guard on a vassal and simulating a degraded data stream, then triggering an AI action and verifying the action is blocked.

**Acceptance Scenarios**:

1. **Given** a vassal has a `data_rate` guard configured with minimum 100bps, **When** the actual data rate drops below 100bps, **Then** the guard enters a failed state.
2. **Given** a guard is in a failed state, **When** an AI session attempts to apply changes to that vassal, **Then** the changes are blocked and the user sees: `"Guard 'data_rate' failed. AI claims rejected."`
3. **Given** a guard fails N consecutive times (circuit breaker threshold), **When** an AI attempts any modification to the guarded vassal, **Then** all modifications are blocked until the user manually acknowledges the alert.

---

### User Story 4 - Guard Configuration (Priority: P3)

A developer configures runtime health checks for their vassals using a simple YAML file in the project. They define port availability checks, log pattern watchers, data rate monitors, and custom validation scripts. The King daemon reads this configuration at startup and begins running checks on the configured intervals.

**Why this priority**: Enables the guard system to be useful — without configuration, guards cannot protect anything. Simple configuration drives open-source adoption.

**Independent Test**: Can be fully tested by writing a `kingdom.yaml` with at least one guard, starting the daemon, and verifying that the guard check runs and produces output.

**Acceptance Scenarios**:

1. **Given** a `kingdom.yaml` with a `port_check` guard for a specific port, **When** the daemon starts, **Then** the guard checks port availability every 5–10 seconds.
2. **Given** a `kingdom.yaml` with a `log_watch` guard listing error patterns, **When** any configured pattern appears in the vassal's output, **Then** the guard triggers a failure.
3. **Given** a `kingdom.yaml` with a `health_check` guard pointing to a script, **When** the script exits with a non-zero code, **Then** the guard records a failure.
4. **Given** no guards are explicitly configured for a vassal, **When** the daemon starts, **Then** the vassal operates normally without any guard checks (guards are opt-in).

---

### Edge Cases

- What happens when the same vassal is requested by two AI sessions simultaneously (race condition)?
- How does the system behave when a guard check script itself crashes or times out?
- What happens when the daemon restarts — does in-memory delegation state reset correctly and do vassals become available again?
- How are guard failures surfaced if the user has no terminal window open?
- What is the default circuit breaker threshold N when not configured in `kingdom.yaml`?
- What happens when a vassal's log output is extremely high volume — does `log_watch` keep up without blocking the vassal?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The daemon MUST allow an AI session to request exclusive delegation control over a specific vassal by providing the vassal ID and the session's process identifier.
- **FR-002**: The daemon MUST reject delegation requests from any session when another session already holds control of that vassal.
- **FR-003**: An AI session MUST be able to explicitly release control of a vassal it currently controls.
- **FR-004**: The daemon MUST maintain delegation state in runtime memory only — this state MUST NOT persist across daemon restarts.
- **FR-005**: The daemon MUST track a heartbeat signal from the controlling AI session and automatically revoke delegation if no heartbeat is received for 30 seconds.
- **FR-006**: Upon automatic revocation of delegation, the daemon MUST resume autonomous management of the affected vassal without requiring user action.
- **FR-007**: Users MUST be able to configure guard checks per vassal in a project-level YAML configuration file (`kingdom.yaml`).
- **FR-008**: The system MUST support at least four guard types: port availability check (`port_check`), log pattern watch (`log_watch`), data rate monitor (`data_rate`), and custom script execution (`health_check`).
- **FR-009**: Guard checks MUST run independently of any AI session — they run continuously as long as the daemon is active.
- **FR-010**: The system MUST implement a circuit breaker: if a guard fails N consecutive times (configurable, default 3), it MUST block all AI-initiated modifications to that vassal.
- **FR-011**: When a circuit breaker activates, the daemon MUST deliver a human-readable notification to the user identifying the failed guard and the blocked action.
- **FR-012**: Guard checks MUST run on a configurable interval (default 5–10 seconds) to limit resource consumption.
- **FR-013**: Guard failures MUST generate descriptive messages clearly stating which guard failed and why (e.g., `"Guard 'data_rate' failed. AI claims rejected."`).
- **FR-014**: The system MUST provide a way to query the current delegation status of any vassal (controller identity, delegation granted time, last heartbeat time).

### Key Entities

- **Vassal**: A managed process within the King kingdom. Can be under daemon control or delegated to an AI session.
- **Delegation**: A time-limited, runtime-only grant of exclusive control over a vassal to a specific AI session.
- **Heartbeat**: A periodic signal sent by the controlling AI session to prove liveness; absence for 30 seconds triggers automatic revocation.
- **Guard**: A runtime health check configured per vassal that runs independently of AI sessions on a regular interval.
- **Circuit Breaker**: A guard escalation mechanism that blocks all AI modifications to a vassal after N consecutive guard failures.
- **Controller**: The entity (daemon or AI session) currently authorized to initiate modifications to a vassal.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: When two AI sessions target the same vassal simultaneously, 100% of conflicting delegation attempts are detected and rejected within 1 second.
- **SC-002**: After an AI session disconnects, the daemon automatically reclaims control within 30–35 seconds in 100% of cases.
- **SC-003**: Guard checks configured in `kingdom.yaml` begin running within 10 seconds of daemon startup.
- **SC-004**: When a circuit breaker activates, 100% of AI modification attempts to the affected vassal are blocked until the user acknowledges.
- **SC-005**: A developer with no prior King experience can configure and activate a `log_watch` guard in under 5 minutes using only the documentation.
- **SC-006**: The guard system adds no more than 5% overhead to daemon resource usage at default check intervals (5–10 seconds).
- **SC-007**: All delegation status queries return a response within 500 milliseconds under normal operating conditions.

## Assumptions

- The King daemon is already running and managing at least one vassal when delegation is requested.
- AI sessions are Claude Code instances that communicate with the King daemon via the existing MCP protocol.
- Heartbeat signals are delivered by the AI session via an MCP tool call at a regular interval (assumed every 10 seconds).
- The `kingdom.yaml` configuration file is located in the project root (same directory as `.king/`).
- Guard checks run within the daemon process and have access to the host system's network and filesystem.
- The circuit breaker threshold N defaults to 3 consecutive failures if not specified in `kingdom.yaml`.
- Data rate guards measure bytes passing through the vassal's output streams (stdout/stderr).
- Custom script guards (`health_check`) execute scripts relative to the project root directory.
- Multi-host scenarios (remote daemons) and mobile support are out of scope for v2.1.
- The existing MCP server infrastructure in King will be extended — no new transport layer is required.
- On daemon restart, all delegation state is cleared and all vassals revert to daemon control automatically.
