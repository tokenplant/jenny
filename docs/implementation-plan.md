# Implementation Plan

Master checklist for implementing headless agent features in Jenny, one at a time. Planned/shipped behavior is documented in `docs/<feature>.md` (e.g. [`agent-loop.md`](./arch/agent-loop.md)).

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
| P0 | [cli](./arch/cli.md) → [anthropic-api-client](./arch/anthropic-api-client.md) → [agent-loop](./arch/agent-loop.md) → [session-persistence](./arch/session-persistence.md) → [session-resume](./arch/session-resume.md) → [sse-streaming](./arch/sse-streaming.md) → [stream-json](./arch/stream-json.md) → [cost-tracking](./arch/cost-tracking.md) → [mcp-config](./arch/mcp-config.md) → [mcp-client](./arch/mcp-client.md) |
| P1 | [tool-registry](./arch/tool-registry.md) → [query-engine](./arch/query-engine.md) → [git-helpers](./tools/git-helpers.md) → [system-prompt](./arch/system-prompt.md) → [read](./tools/read.md) → [glob](./tools/glob.md) → [grep](./tools/grep.md) → [dangerous-command-gate](./tools/dangerous-command-gate.md) → [bash](./tools/bash.md) → [write](./tools/write.md) → [edit](./tools/edit.md) → [parallel-tool-execution](./arch/parallel-tool-execution.md) → [rate-limit-handling](./arch/rate-limit-handling.md) |
| P2 | [message-normalization](./arch/message-normalization.md) → [context-compaction](./arch/context-compaction.md) → [sandbox](./patterns/sandbox.md) → [notebook-edit](./tools/notebook-edit.md) → [list-mcp-resources](./tools/list-mcp-resources.md) → [read-mcp-resource](./tools/read-mcp-resource.md) |
| P3 | [structured-logging](./arch/structured-logging.md) → [memdir](./patterns/memdir.md) → [session-memory](./arch/session-memory.md) → [memory-extraction](./patterns/memory-extraction.md) → [web-fetch](./tools/web-fetch.md) → [web-search](./tools/web-search.md) → [lsp](./tools/lsp.md) → [skill](./patterns/skill.md) → [sleep-await](./tools/sleep-await.md) |
| P4 | [subagent-types](./arch/subagent-types.md) → [tasks](./tools/tasks.md) → [agent-resume-fork](./patterns/agent-resume-fork.md) → [background-tasks](./patterns/background-tasks.md) → [skills-framework](./patterns/skills-framework.md) → [structured-sdk-output](./arch/structured-sdk-output.md) → [swarm](./patterns/swarm.md) → [enter-worktree](./tools/enter-worktree.md) → [exit-worktree](./tools/exit-worktree.md) |

---

## Checklist

### P0 — Headless operator contract

Implement in this order:

#### Engine

1. - [x] Session ID stability
   - SessionID() returns error (not empty string or PID-based) when crypto/rand fails
   - Path traversal validation on session IDs
   - _(No separate spec; covered by [session-persistence.md](./arch/session-persistence.md) / [session-resume.md](./arch/session-resume.md))_

2. - [x] CLI (`-p`, `--model`, flags) — [`cli.md`](./arch/cli.md)
   - No prompt → print usage, exit non-zero
   - Positional prompt and `-p` are mutually exclusive (positional wins when both given)
   - `--model` overrides `ANTHROPIC_MODEL`
   - `-r` wired for session resume
   - `--mcp-config` wired (AC2)
   - `--continue` wired (AC1-AC10)

3. - [x] Anthropic API client — [`anthropic-api-client.md`](./arch/anthropic-api-client.md)
   - System prompt is a top-level parameter, not a `role: system` message
   - Assistant messages with `tool_use` must include the full `tool_use` block before tool results
   - Tool results go in user messages as `tool_result` blocks keyed by `tool_use_id`
   - Image validation and SSE streaming default both shipped ✓
   - [x] DeepSeek tool_result dedup fix — duplicate `tool_result` blocks deduplicated by `tool_use_id` in `mergeConsecutiveSameRole` and SDK serialization safety net ✓
   - [x] Document provider-aware fix rationale — see [provider-aware-fixes.md](./arch/provider-aware-fixes.md)
   - [x] Universal normalization: make existing shims (DeepSeek dedup, MiniMax __arg__) apply to all providers; remove URL-based provider detection — see [universal-normalization-architecture.md](./arch/universal-normalization-architecture.md)
   - [ ] Full SSNF pipeline (deferred — see [universal-normalization-architecture.md](./arch/universal-normalization-architecture.md))

4. - [x] Core agent loop — partial — [`agent-loop.md`](./arch/agent-loop.md)
   - Basic tool_use → execute → tool_result loop
   - Unknown tool → immediate error (does not hang)
   - **Deferred:** thinking/interrupt/spill/compaction handling → P3+

5. - [x] Session persistence — [session-persistence.md](./arch/session-persistence.md)
   - JSONL transcript per project directory ✓
   - Progress/ephemeral message types are not chain nodes (do not fork conversation on reload) ✓
   - Tombstone rewrite capped to prevent OOM on huge sessions ✓
   - Flush on shutdown; respect persistence-disable flag ✓


6. - [x] Session resume (`-r`) — partial — [`session-resume.md`](./arch/session-resume.md)
   - Rebuild message history from transcript ✓
   - **Deferred:** compaction boundaries → P3+

7. - [x] SSE streaming from API — [`sse-streaming.md`](./arch/sse-streaming.md) _(moved from P2)_
   - Stream via server-sent events; yield partial text as it arrives ✓
   - On streaming failure with fallback configured, retry non-streaming ✓
   - Emit `stream_request_start` before each API iteration ✓

8. - [x] Stream-json output (NDJSON) — [`stream-json.md`](./arch/stream-json.md)
   - One JSON object per line on stdout; debug on stderr
   - Terminal `{ type: "result", session_id, result, usage }` on successful run
   - stdout guard implemented (AC3)
   - [x] Stream-json output: aggregated user/assistant/result events + parent_tool_use_id field; field order matches reference

9. - [x] Cost / token tracking — [`cost-tracking.md`](./arch/cost-tracking.md)
   - Track input/output tokens plus cache read/creation per model
   - Persist cost state keyed to session ID; restore only on matching resume
   - Emit usage in stream-json `result` line (`cache_read_input_tokens`, `cache_creation_input_tokens`)
   - All ACs implemented and tested ✓

10. - [x] MCP config (`--mcp-config`) — [`mcp-config.md`](./arch/mcp-config.md)
    - Load multiple config files; expand env vars in server definitions
    - **AC1-AC5:** All acceptance criteria implemented and tested ✓

11. - [x] MCP client — [`mcp-client.md`](./arch/mcp-client.md) — partial
    - **stdio transport:** connect, initialize, tool discovery, tool dispatch ✓
    - **AC1-AC5:** All ACs implemented and tested (stdio only this iteration)
    - **Deferred:** SSE/HTTP/WebSocket, OAuth, binary persistence, content truncation, resource cache, progress events → P4 (per mcp-client.md defer_to: P4)

#### Tools

_(none at P0 — coding tools start at P1)_

---

### P1 — Autonomous coding

Implement in this order:

#### Engine

1. - [x] Default tool preset / registry — [`tool-registry.md`](./arch/tool-registry.md)
2. - [x] QueryEngine lifecycle — [`query-engine.md`](./arch/query-engine.md) — done
   - Persist user message to transcript before API loop ✓
   - AC1-AC5: persist-before-API, maxTurns, flush on completion, RunStream refactor, turn counter ✓
   - AC4: WireReadFileCache functional at engine.go:109 ✓
   - **Deferred:** cross-turn state (readFileState round-trip, maxBudgetUsd method, permissionDenials queue) → P3
3. - [x] Git helpers — [`git-helpers.md`](./tools/git-helpers.md) _(moved from P2)_
4. - [x] System prompt assembly — [`system-prompt.md`](./arch/system-prompt.md) — AC1-AC5 implemented
5. - [x] Parallel tool execution — [`parallel-tool-execution.md`](./arch/parallel-tool-execution.md) _(after tools below)_
6. - [x] Rate limit handling — [`rate-limit-handling.md`](./arch/rate-limit-handling.md)

#### Tools

7. - [x] Read — partial — **Deferred:** polish (size limits, images/PDF, dedup, block device guard) → P3 — [`read.md`](./tools/read.md)
8. - [x] Glob — [`glob.md`](./tools/glob.md)
9. - [x] Grep — [`grep.md`](./tools/grep.md) _(host ripgrep first; sandboxed path in P2)_
10. - [x] Dangerous command gate — [`dangerous-command-gate.md`](./tools/dangerous-command-gate.md)
11. - [x] Bash (full) — [`bash.md`](./tools/bash.md) _(includes former read-only baseline)_
    - Classifier + sandbox; output spill; sed simulation; `run_in_background`
12. - [x] Write — [`write.md`](./tools/write.md)
13. - [x] Edit — [`edit.md`](./tools/edit.md)

Then return to engine items 5–6 (parallel execution, rate limits).

---

### P2 — Long sessions & hardening

Implement in this order:

#### Engine

1. - [x] Message normalization — [`message-normalization.md`](./arch/message-normalization.md) — AC1-AC5 implemented
2. - [x] Context compaction — [`context-compaction.md`](./arch/context-compaction.md)
   - [x] Disambiguate max_tokens stop reason (output cap vs. context exhaustion) — [`context-compaction.md#error-reporting-stop_reason-max_tokens`](./arch/context-compaction.md#error-reporting-stop_reason-max_tokens)
3. - [x] Sandbox abstraction — [`sandbox.md`](./patterns/sandbox.md)
4. _(Git helpers — shipped in P1)_

#### Tools

5. - [x] Notebook edit — [`notebook-edit.md`](./tools/notebook-edit.md)
6. - [x] ListMcpResources — [`list-mcp-resources.md`](./tools/list-mcp-resources.md) — partial
7. - [x] ReadMcpResource — [`read-mcp-resource.md`](./tools/read-mcp-resource.md) — partial

---

### P3 — Optional enhancements

#### Engine

- [x] Memdir — [`memdir.md`](./patterns/memdir.md)
- [x] Session memory — [`session-memory.md`](./arch/session-memory.md)
- [x] Memory extraction — [`memory-extraction.md`](./patterns/memory-extraction.md)
- [x] Structured logging — partial — [`structured-logging.md`](./arch/structured-logging.md)

#### Tools

- [x] WebFetch — [`web-fetch.md`](./tools/web-fetch.md)
- [x] WebSearch — [`web-search.md`](./tools/web-search.md)
- [x] LSP — [`lsp.md`](./tools/lsp.md)
- [x] Skill — [`skill.md`](./patterns/skill.md)
- [x] Sleep / Await — [`sleep-await.md`](./tools/sleep-await.md)

---

### P4 — Defer (multi-agent / orchestration surface)

#### Engine

- [x] Subagent types — [`subagent-types.md`](./arch/subagent-types.md)
- [x] Tasks (merged) — [`tasks.md`](./tools/tasks.md)
- [x] Agent resume / fork — [`agent-resume-fork.md`](./patterns/agent-resume-fork.md)
- [x] Background tasks — [`background-tasks.md`](./patterns/background-tasks.md)
- [x] Skills framework — [`skills-framework.md`](./patterns/skills-framework.md)
- [x] Structured SDK output — [`structured-sdk-output.md`](./arch/structured-sdk-output.md)
- [x] Swarm — [`swarm.md`](./patterns/swarm.md)

#### Tools

- [x] Tasks (TodoWrite, TaskCreate, TaskGet, TaskList, TaskUpdate, TaskStop, TaskOutput, TaskSubAgent) — [`tasks.md`](./tools/tasks.md)
- [x] EnterWorktree / ExitWorktree — [`enter-worktree.md`](./tools/enter-worktree.md), [`exit-worktree.md`](./tools/exit-worktree.md)

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
