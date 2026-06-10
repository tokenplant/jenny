# Jenny Feature Specifications

Clean-room specifications for headless agent implementation. Each doc is the source of truth for its feature: read the spec, write tests, then implement in Go.

**Workflow:** Documentation → Tests (`internal/**/*_test.go`) → Code

**Master checklist and order:** [`implementation-plan.md`](./implementation-plan.md)

## Frontmatter

Every spec file starts with YAML frontmatter:

```yaml
---
title: Read Tool
slug: read
priority: P1
status: partial          # not_started | partial | done
spec: complete           # spec document finished
code: partial # Go code in internal/
package: internal/tool
gaps:
  - Naive line scan
  - No size/token limits
depends_on:
  - tool-registry
---
```

| Field | Meaning |
|-------|---------|
| `status` | Overall feature readiness (`not_started`, `partial`, `done`) |
| `spec` | Spec completeness (`complete` for all listed features) |
| `code` | Implementation state in the repo |
| `gaps` | Known missing behavior vs spec (empty when none) |
| `depends_on` | Slug(s) to implement first |
| `package` | Target Go package(s) |

Filter specs by status:

```bash
grep -l '^status: partial' docs/arch/*.md docs/tools/*.md
grep -l '^status: not_started' docs/arch/*.md docs/tools/*.md
```

## Protocol compatibility

Headless operators depend on exact behavior for:

- [`stream-json.md`](./arch/stream-json.md) — NDJSON stdout protocol (requires [`sse-streaming.md`](./arch/sse-streaming.md) for full compliance)
- [`cli.md`](./arch/cli.md) — CLI flags and exit codes
- [`cost-tracking.md`](./arch/cost-tracking.md) — Terminal `result.usage` fields

## Core Architecture

Specifications for the core agent engine and infrastructure.

| Spec | Description |
|------|-------------|
| [agent-loop.md](./arch/agent-loop.md) | Core tool_use → execute → tool_result loop |
| [anthropic-api-client.md](./arch/anthropic-api-client.md) | API client with message normalization |
| [cli.md](./arch/cli.md) | CLI flags and exit codes |
| [context-compaction.md](./arch/context-compaction.md) | Long-session context management |
| [cost-tracking.md](./arch/cost-tracking.md) | Token and cost tracking |
| [e2e-test-harness.md](./arch/e2e-test-harness.md) | End-to-end testing infrastructure |
| [mcp-client.md](./arch/mcp-client.md) | MCP stdio client implementation |
| [mcp-config.md](./arch/mcp-config.md) | MCP server configuration |
| [message-normalization.md](./arch/message-normalization.md) | Cross-provider message normalization |
| [parallel-tool-execution.md](./arch/parallel-tool-execution.md) | Concurrent tool execution |
| [provider-aware-fixes.md](./arch/provider-aware-fixes.md) | Provider-specific API fix rationale |
| [query-engine.md](./arch/query-engine.md) | QueryEngine lifecycle and state |
| [rate-limit-handling.md](./arch/rate-limit-handling.md) | API retry and rate limit handling |
| [session-memory.md](./arch/session-memory.md) | Session-scoped memory storage |
| [session-persistence.md](./arch/session-persistence.md) | JSONL transcript persistence |
| [session-resume.md](./arch/session-resume.md) | Session resume from transcript |
| [sse-streaming.md](./arch/sse-streaming.md) | Server-sent events streaming |
| [stream-json-spec.md](./arch/stream-json-spec.md) | NDJSON output format specification |
| [stream-json.md](./arch/stream-json.md) | NDJSON output implementation |
| [structured-logging.md](./arch/structured-logging.md) | Structured log output |
| [structured-sdk-output.md](./arch/structured-sdk-output.md) | SDK result serialization |
| [subagent-types.md](./arch/subagent-types.md) | Subagent type definitions |
| [system-prompt.md](./arch/system-prompt.md) | System prompt assembly |
| [testutil.md](./arch/testutil.md) | Shared test helpers and import cycle resolution |
| [tool-registry.md](./arch/tool-registry.md) | Default tool preset registry |
| [universal-normalization-architecture.md](./arch/universal-normalization-architecture.md) | Cross-provider normalization architecture |

## Tool Specifications

Specifications for individual tool implementations.

| Spec | Description |
|------|-------------|
| [bash.md](./tools/bash.md) | Shell command execution with sandbox |
| [dangerous-command-gate.md](./tools/dangerous-command-gate.md) | Security gate for dangerous commands |
| [edit.md](./tools/edit.md) | In-place file editing |
| [enter-worktree.md](./tools/enter-worktree.md) | Enter git worktree for isolation |
| [exit-worktree.md](./tools/exit-worktree.md) | Exit git worktree |
| [git-helpers.md](./tools/git-helpers.md) | Git status and operations |
| [glob.md](./tools/glob.md) | File pattern matching |
| [grep.md](./tools/grep.md) | Content search with ripgrep |
| [list-mcp-resources.md](./tools/list-mcp-resources.md) | List MCP server resources |
| [lsp.md](./tools/lsp.md) | Language Server Protocol integration |
| [notebook-edit.md](./tools/notebook-edit.md) | Jupyter notebook editing |
| [read-mcp-resource.md](./tools/read-mcp-resource.md) | Read MCP resource content |
| [read.md](./tools/read.md) | File content reading |
| [sleep-await.md](./tools/sleep-await.md) | Sleep and await primitives |
| [tasks.md](./tools/tasks.md) | Task/subagent and todo tools (merged) |
| [web-fetch.md](./tools/web-fetch.md) | HTTP content fetching |
| [web-search.md](./tools/web-search.md) | Web search integration |
| [write.md](./tools/write.md) | File writing |

## Design Patterns

Specifications for architectural patterns and optional subsystems.

| Spec | Description |
|------|-------------|
| [agent-resume-fork.md](./patterns/agent-resume-fork.md) | Agent forking and resume |
| [background-tasks.md](./patterns/background-tasks.md) | Background task handling |
| [memdir.md](./patterns/memdir.md) | Memory directory management |
| [memory-extraction.md](./patterns/memory-extraction.md) | Memory extraction patterns |
| [sandbox.md](./patterns/sandbox.md) | Sandboxed execution abstraction |
| [skill.md](./patterns/skill.md) | Skill definition and execution |
| [skills-framework.md](./patterns/skills-framework.md) | Skills framework architecture |
| [swarm.md](./patterns/swarm.md) | Multi-agent swarm orchestration |

## Implementation Order

### P0 — Headless operator contract

CLI → API → loop → persistence → resume → SSE → stream-json → cost → MCP

| # | Feature | Spec | Status |
|---|---------|------|--------|
| 1 | CLI | [cli.md](./arch/cli.md) | partial |
| 2 | Anthropic API client | [anthropic-api-client.md](./arch/anthropic-api-client.md) | partial |
| 3 | Core agent loop | [agent-loop.md](./arch/agent-loop.md) | partial |
| 4 | Session persistence | [session-persistence.md](./arch/session-persistence.md) | partial |
| 5 | Session resume | [session-resume.md](./arch/session-resume.md) | partial |
| 6 | SSE streaming | [sse-streaming.md](./arch/sse-streaming.md) | partial |
| 7 | Stream-json | [stream-json.md](./arch/stream-json.md) | partial |
| 8 | Cost tracking | [cost-tracking.md](./arch/cost-tracking.md) | done |
| 9 | MCP config | [mcp-config.md](./arch/mcp-config.md) | partial |
| 10 | MCP client | [mcp-client.md](./arch/mcp-client.md) | partial |

### P1 — Autonomous coding

Registry → QueryEngine → git → system prompt → Read/Glob/Grep → security → Bash → Write/Edit → parallel → rate limits

| Feature | Spec |
|---------|------|
| Tool registry | [tool-registry.md](./arch/tool-registry.md) |
| QueryEngine | [query-engine.md](./arch/query-engine.md) |
| Git helpers | [git-helpers.md](./tools/git-helpers.md) |
| System prompt | [system-prompt.md](./arch/system-prompt.md) |
| Read / Glob / Grep | [read.md](./tools/read.md), [glob.md](./tools/glob.md), [grep.md](./tools/grep.md) |
| Dangerous command gate | [dangerous-command-gate.md](./tools/dangerous-command-gate.md) |
| Bash | [bash.md](./tools/bash.md) |
| Write / Edit | [write.md](./tools/write.md), [edit.md](./tools/edit.md) |
| Parallel tools | [parallel-tool-execution.md](./arch/parallel-tool-execution.md) |
| Rate limits | [rate-limit-handling.md](./arch/rate-limit-handling.md) |

### P2 — Long sessions & hardening

| Feature | Spec |
|---------|------|
| Message normalization | [message-normalization.md](./arch/message-normalization.md), [universal-normalization-architecture.md](./arch/universal-normalization-architecture.md) |
| Context compaction | [context-compaction.md](./arch/context-compaction.md) |
| Sandbox | [sandbox.md](./patterns/sandbox.md) |
| Notebook edit | [notebook-edit.md](./tools/notebook-edit.md) |
| MCP resources | [list-mcp-resources.md](./tools/list-mcp-resources.md), [read-mcp-resource.md](./tools/read-mcp-resource.md) |

### P3 — Optional enhancements

[structured-logging.md](./arch/structured-logging.md), [memdir.md](./patterns/memdir.md), [session-memory.md](./arch/session-memory.md), [memory-extraction.md](./patterns/memory-extraction.md), [web-fetch.md](./tools/web-fetch.md), [web-search.md](./tools/web-search.md), [lsp.md](./tools/lsp.md), [skill.md](./patterns/skill.md), [sleep-await.md](./tools/sleep-await.md)

### P4 — Multi-agent / orchestration

[subagent-types.md](./arch/subagent-types.md), [tasks.md](./tools/tasks.md), [agent-resume-fork.md](./patterns/agent-resume-fork.md), [background-tasks.md](./patterns/background-tasks.md), [skills-framework.md](./patterns/skills-framework.md), [structured-sdk-output.md](./arch/structured-sdk-output.md), [swarm.md](./patterns/swarm.md)