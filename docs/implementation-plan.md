# Implementation Plan

Master checklist for implementing headless agent features in Jenny, one at a time. Planned/shipped behavior is documented in `docs/<feature>.md` (e.g. [`agent-loop.md`](./agent-loop.md)).

Items are ordered **P0 → P4** by dependency and what a headless, unattended coding agent needs first. **Within each tier, follow the numbered order** — later items may depend on earlier ones in the same tier.

## Workflow

Every feature follows this order — never skip or reorder:

1. **Documentation** — `docs/<feature>.md` (YAML frontmatter records spec/code status)
2. **Tests** — `internal/**/*_test.go`
3. **Code** — implementation matching spec and tests

Project name, version, and user-facing strings live in `internal/constants/` so the binary can be renamed easily.

## Ordering rationale

| Decision | Why |
|----------|-----|
| **P0: CLI → API client → agent loop first** | Minimal runnable path before persistence or stream-json polish |
| **P0: Session persistence before resume** | Resume reads transcripts that persistence writes |
| **P0: SSE streaming before stream-json (moved from P2)** | `include_partial_messages`, `stream_event`, and live token streaming depend on API SSE; stream-json cannot be fully wire-compatible without it |
| **P0: Stream-json before cost tracking** | Terminal `result` line is the cost/usage emission surface |
| **P0: MCP config → client last in P0** | Extends the working headless agent; not required for Read/Bash coding loop |
| **P1: Tool registry → QueryEngine before tools** | Registry defines the tool surface; QueryEngine owns `readFileState` and persist-before-API |
| **P1: Git helpers before system prompt (moved from P2)** | System prompt injects git status; helpers avoid shelling out ad hoc |
| **P1: Read → Glob → Grep before Write/Edit** | Discovery and read-before-write contract before mutation |
| **P1: Dangerous command gate before Bash (full)** | Security layer must wrap shell execution |
| **P1: Parallel tools after sequential tools work** | Optimization once correctness is proven |
| **P1: Rate limits after core loop** | Retries harden an working API path |
| **P2: Message normalization before compaction** | Compaction rewrite must satisfy pairing/thinking rules |
| **P2: Sandbox before production Grep hardening** | Sandboxed ripgrep path; P1 Grep can ship with host `rg` first |
| **P3 / P4 unchanged in tier** | Optional memory, web, LSP; multi-agent orchestration deferred |

### Frontmatter (spec documents)

Each `docs/<feature>.md` begins with YAML:

| Field | Values | Meaning |
|-------|--------|---------|
| `status` | `not_started`, `partial`, `done` | Overall feature readiness |
| `spec` | `complete` | Spec written (always, for listed features) |
| `code` | `not_started`, `partial`, `done` | Go implementation in `internal/` |
| `gaps` | list | Known missing behavior vs spec |
| `depends_on` | list of slugs | Implement after these features |

## Specification index

All feature specs live in `docs/`. See [`README.md`](./README.md) for the full index.

| Tier | Specs (implementation order) |
|------|------------------------------|
| P0 | [cli](./cli.md) → [anthropic-api-client](./anthropic-api-client.md) → [agent-loop](./agent-loop.md) → [session-persistence](./session-persistence.md) → [session-resume](./session-resume.md) → [sse-streaming](./sse-streaming.md) → [stream-json](./stream-json.md) → [cost-tracking](./cost-tracking.md) → [mcp-config](./mcp-config.md) → [mcp-client](./mcp-client.md) |
| P1 | [tool-registry](./tool-registry.md) → [query-engine](./query-engine.md) → [git-helpers](./git-helpers.md) → [system-prompt](./system-prompt.md) → [read](./read.md) → [glob](./glob.md) → [grep](./grep.md) → [dangerous-command-gate](./dangerous-command-gate.md) → [bash](./bash.md) → [write](./write.md) → [edit](./edit.md) → [parallel-tool-execution](./parallel-tool-execution.md) → [rate-limit-handling](./rate-limit-handling.md) |
| P2 | [message-normalization](./message-normalization.md) → [context-compaction](./context-compaction.md) → [sandbox](./sandbox.md) → [notebook-edit](./notebook-edit.md) → [list-mcp-resources](./list-mcp-resources.md) → [read-mcp-resource](./read-mcp-resource.md) |
| P3 | [structured-logging](./structured-logging.md) → [memdir](./memdir.md) → [session-memory](./session-memory.md) → [memory-extraction](./memory-extraction.md) → [web-fetch](./web-fetch.md) → [web-search](./web-search.md) → [lsp](./lsp.md) → [skill](./skill.md) → [sleep-await](./sleep-await.md) |
| P4 | [subagent-types](./subagent-types.md) → [task-subagent](./task-subagent.md) → [agent-resume-fork](./agent-resume-fork.md) → [background-tasks](./background-tasks.md) → [task-output](./task-output.md) → [task-stop](./task-stop.md) → [skills-framework](./skills-framework.md) → [structured-sdk-output](./structured-sdk-output.md) → [swarm](./swarm.md) → [todo-write](./todo-write.md) → Task v2 tools → [enter-worktree](./enter-worktree.md) → [exit-worktree](./exit-worktree.md) |

---

## Checklist

### P0 — Headless operator contract

Implement in this order:

#### Engine

1. - [x] Session ID stability
   - SessionID() returns error (not empty string or PID-based) when crypto/rand fails
   - Path traversal validation on session IDs
   - _(No separate spec; covered by [session-persistence.md](./session-persistence.md) / [session-resume.md](./session-resume.md))_

2. - [x] CLI (`-p`, `--model`, flags) — [`cli.md`](./cli.md)
   - No prompt → print usage, exit non-zero
   - Positional prompt and `-p` are mutually exclusive (positional wins when both given)
   - `--model` overrides `ANTHROPIC_MODEL`
   - `-r` wired for session resume
   - `--mcp-config` wired (AC2)
   - `--continue` wired (AC1-AC10)

3. - [x] Anthropic API client — [`anthropic-api-client.md`](./anthropic-api-client.md)
   - System prompt is a top-level parameter, not a `role: system` message
   - Assistant messages with `tool_use` must include the full `tool_use` block before tool results
   - Tool results go in user messages as `tool_result` blocks keyed by `tool_use_id`
   - Image validation and SSE streaming default both shipped ✓
   - [x] DeepSeek tool_result dedup fix — duplicate `tool_result` blocks deduplicated by `tool_use_id` in `mergeConsecutiveSameRole` and SDK serialization safety net ✓

4. - [x] Core agent loop — partial — [`agent-loop.md`](./agent-loop.md)
   - Basic tool_use → execute → tool_result loop
   - Unknown tool → immediate error (does not hang)
   - **Deferred:** thinking/interrupt/spill/compaction handling → P3+

5. - [x] Session persistence — [session-persistence.md](./session-persistence.md)
   - JSONL transcript per project directory ✓
   - Progress/ephemeral message types are not chain nodes (do not fork conversation on reload) ✓
   - Tombstone rewrite capped to prevent OOM on huge sessions ✓
   - Flush on shutdown; respect persistence-disable flag ✓


6. - [x] Session resume (`-r`) — partial — [`session-resume.md`](./session-resume.md)
   - Rebuild message history from transcript ✓
   - **Deferred:** compaction boundaries → P3+

7. - [x] SSE streaming from API — [`sse-streaming.md`](./sse-streaming.md) _(moved from P2)_
   - Stream via server-sent events; yield partial text as it arrives ✓
   - On streaming failure with fallback configured, retry non-streaming ✓
   - Emit `stream_request_start` before each API iteration ✓

8. - [x] Stream-json output (NDJSON) — [`stream-json.md`](./stream-json.md)
   - One JSON object per line on stdout; debug on stderr
   - Terminal `{ type: "result", session_id, result, usage }` on successful run
   - stdout guard implemented (AC3)

9. - [x] Cost / token tracking — [`cost-tracking.md`](./cost-tracking.md)
   - Track input/output tokens plus cache read/creation per model
   - Persist cost state keyed to session ID; restore only on matching resume
   - Emit usage in stream-json `result` line (`cache_read_input_tokens`, `cache_creation_input_tokens`)
   - All ACs implemented and tested ✓

10. - [x] MCP config (`--mcp-config`) — [`mcp-config.md`](./mcp-config.md)
    - Load multiple config files; expand env vars in server definitions
    - **AC1-AC5:** All acceptance criteria implemented and tested ✓

11. - [x] MCP client — [`mcp-client.md`](./mcp-client.md) — partial
    - **stdio transport:** connect, initialize, tool discovery, tool dispatch ✓
    - **AC1-AC5:** All ACs implemented and tested (stdio only this iteration)
    - **Deferred:** SSE/HTTP/WebSocket, OAuth, binary persistence, content truncation, resource cache, progress events → P4 (per mcp-client.md defer_to: P4)

#### Tools

_(none at P0 — coding tools start at P1)_

---

### P1 — Autonomous coding

Implement in this order:

#### Engine

1. - [x] Default tool preset / registry — [`tool-registry.md`](./tool-registry.md)
2. - [x] QueryEngine lifecycle — [`query-engine.md`](./query-engine.md) — done
   - Persist user message to transcript before API loop ✓
   - AC1-AC5: persist-before-API, maxTurns, flush on completion, RunStream refactor, turn counter ✓
   - AC4: WireReadFileCache functional at engine.go:109 ✓
   - **Deferred:** cross-turn state (readFileState round-trip, maxBudgetUsd method, permissionDenials queue) → P3
3. - [x] Git helpers — [`git-helpers.md`](./git-helpers.md) _(moved from P2)_
4. - [x] System prompt assembly — [`system-prompt.md`](./system-prompt.md) — AC1-AC5 implemented
5. - [x] Parallel tool execution — [`parallel-tool-execution.md`](./parallel-tool-execution.md) _(after tools below)_
6. - [x] Rate limit handling — [`rate-limit-handling.md`](./rate-limit-handling.md)

#### Tools

7. - [x] Read — partial — **Deferred:** polish (size limits, images/PDF, dedup, block device guard) → P3 — [`read.md`](./read.md)
8. - [x] Glob — [`glob.md`](./glob.md)
9. - [x] Grep — [`grep.md`](./grep.md) _(host ripgrep first; sandboxed path in P2)_
10. - [x] Dangerous command gate — [`dangerous-command-gate.md`](./dangerous-command-gate.md)
11. - [x] Bash (full) — [`bash.md`](./bash.md) _(includes former read-only baseline)_
    - Classifier + sandbox; output spill; sed simulation; `run_in_background`
12. - [x] Write — [`write.md`](./write.md)
13. - [x] Edit — [`edit.md`](./edit.md)

Then return to engine items 5–6 (parallel execution, rate limits).

---

### P2 — Long sessions & hardening

Implement in this order:

#### Engine

1. - [x] Message normalization — [`message-normalization.md`](./message-normalization.md) — AC1-AC5 implemented
2. - [x] Context compaction — [`context-compaction.md`](./context-compaction.md)
3. - [x] Sandbox abstraction — [`sandbox.md`](./sandbox.md)
4. _(Git helpers — shipped in P1)_

#### Tools

5. - [x] Notebook edit — [`notebook-edit.md`](./notebook-edit.md)
6. - [x] ListMcpResources — [`list-mcp-resources.md`](./list-mcp-resources.md) — partial
7. - [x] ReadMcpResource — [`read-mcp-resource.md`](./read-mcp-resource.md) — partial

---

### P3 — Optional enhancements

#### Engine

- [x] Memdir — [`memdir.md`](./memdir.md)
- [x] Session memory — [`session-memory.md`](./session-memory.md)
- [x] Memory extraction — [`memory-extraction.md`](./memory-extraction.md)
- [x] Structured logging — partial — [`structured-logging.md`](./structured-logging.md)

#### Tools

- [x] WebFetch — [`web-fetch.md`](./web-fetch.md)
- [x] WebSearch — [`web-search.md`](./web-search.md)
- [x] LSP — [`lsp.md`](./lsp.md)
- [x] Skill — [`skill.md`](./skill.md)
- [x] Sleep / Await — [`sleep-await.md`](./sleep-await.md)

---

### P4 — Defer (multi-agent / orchestration surface)

#### Engine

- [x] Subagent types — [`subagent-types.md`](./subagent-types.md)
- [x] Task (subagent) — [`task-subagent.md`](./task-subagent.md)
- [x] Agent resume / fork — [`agent-resume-fork.md`](./agent-resume-fork.md)
- [x] Background tasks — [`background-tasks.md`](./background-tasks.md)
- [x] Skills framework — [`skills-framework.md`](./skills-framework.md)
- [x] Structured SDK output — [`structured-sdk-output.md`](./structured-sdk-output.md)
- [x] Swarm — [`swarm.md`](./swarm.md)

#### Tools

- [x] TodoWrite — [`todo-write.md`](./todo-write.md)
- [x] TaskCreate — [`task-create.md`](./task-create.md)
- [x] TaskGet — [`task-get.md`](./task-get.md)
- [x] TaskList — [`task-list.md`](./task-list.md)
- [x] TaskUpdate — [`task-update.md`](./task-update.md)
- [x] TaskStop — [`task-stop.md`](./task-stop.md)
- [x] TaskOutput — [`task-output.md`](./task-output.md)
- [x] EnterWorktree / ExitWorktree — [`enter-worktree.md`](./enter-worktree.md), [`exit-worktree.md`](./exit-worktree.md)

---

## Out of Scope

- TUI, Ink, REPL, voice, vim, keybindings
- Slash commands
- Remote control, bridge, analytics, telemetry
- AskUserQuestion, plan mode enter/exit
- PowerShell
- Team / coordinator / SendMessage
- Cron / remote triggers
- Interactive config tool
- WebBrowser, computer use, Chrome integration
- Brief, synthetic, overflow test tools
