# Security Research

This directory documents defensive lessons, attack surfaces, and mitigation strategies for agentic developer systems built on Claude King.

## Purpose

Claude King orchestrates multiple AI coding agents across repositories. As with any system that executes code on behalf of AI models, the attack surface is real and worth documenting clearly.

This directory does not aim to sensationalize vulnerabilities or demonstrate exploits. It aims to:

- Identify realistic risk surfaces in the current implementation
- Document existing safeguards and their coverage
- Highlight gaps that remain unaddressed
- Propose concrete, incremental mitigations

## Scope

All analysis is grounded in the actual Claude King codebase. Where evidence is incomplete or behavior is inferred rather than verified, this is stated explicitly.

## Documents

| File | Topic |
|------|-------|
| [command-execution-risks.md](command-execution-risks.md) | Risks from `exec_in`, PTY sessions, and delegation |
| [permission-model-review.md](permission-model-review.md) | Coverage gaps in the approval and delegation model |
| [artifact-exposure-prevention.md](artifact-exposure-prevention.md) | Artifact secret scanning: coverage, limits, gaps |

## Contributing

Security findings should be documented using the standard structure:

- **Summary** — one paragraph
- **Scope** — what code paths / features are covered
- **Relevant Components** — exact file paths
- **Risk Description** — what can go wrong
- **Abuse Scenario** — realistic path to harm (not a proof-of-concept exploit)
- **Existing Safeguards** — what the code already does
- **Gaps** — what is not covered
- **Recommendations** — concrete, incremental steps
