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
code: partial            # Go code in internal/
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
grep -l '^status: partial' docs/*.md
grep -l '^status: not_started' docs/*.md
```

## Protocol compatibility

Headless operators depend on exact behavior for:

- [`stream-json.md`](./stream-json.md) — NDJSON stdout protocol (requires [`sse-streaming.md`](./sse-streaming.md) for full compliance)
- [`cli.md`](./cli.md) — CLI flags and exit codes
- [`cost-tracking.md`](./cost-tracking.md) — Terminal `result.usage` fields

## Specification index (implementation order)

### P0 — Headless operator contract

CLI → API → loop → persistence → resume → SSE → stream-json → cost → MCP

| # | Feature | Spec | Status |
|---|---------|------|--------|
| 1 | CLI | [cli.md](./cli.md) | partial |
| 2 | Anthropic API client | [anthropic-api-client.md](./anthropic-api-client.md) | partial |
| 3 | Core agent loop | [agent-loop.md](./agent-loop.md) | partial |
| 4 | Session persistence | [session-persistence.md](./session-persistence.md) | partial |
| 5 | Session resume | [session-resume.md](./session-resume.md) | partial |
| 6 | SSE streaming | [sse-streaming.md](./sse-streaming.md) | partial |
| 7 | Stream-json | [stream-json.md](./stream-json.md) | partial |
| 8 | Cost tracking | [cost-tracking.md](./cost-tracking.md) | done |
| 9 | MCP config | [mcp-config.md](./mcp-config.md) | partial |
| 10 | MCP client | [mcp-client.md](./mcp-client.md) | partial |

### P1 — Autonomous coding

Registry → QueryEngine → git → system prompt → Read/Glob/Grep → security → Bash → Write/Edit → parallel → rate limits

| Feature | Spec |
|---------|------|
| Tool registry | [tool-registry.md](./tool-registry.md) |
| QueryEngine | [query-engine.md](./query-engine.md) |
| Git helpers | [git-helpers.md](./git-helpers.md) |
| System prompt | [system-prompt.md](./system-prompt.md) |
| Read / Glob / Grep | [read.md](./read.md), [glob.md](./glob.md), [grep.md](./grep.md) |
| Dangerous command gate | [dangerous-command-gate.md](./dangerous-command-gate.md) |
| Bash | [bash.md](./bash.md) |
| Write / Edit | [write.md](./write.md), [edit.md](./edit.md) |
| Parallel tools | [parallel-tool-execution.md](./parallel-tool-execution.md) |
| Rate limits | [rate-limit-handling.md](./rate-limit-handling.md) |

### P2 — Long sessions & hardening

| Feature | Spec |
|---------|------|
| Message normalization | [message-normalization.md](./message-normalization.md) |
| Context compaction | [context-compaction.md](./context-compaction.md) |
| Sandbox | [sandbox.md](./sandbox.md) |
| Notebook edit | [notebook-edit.md](./notebook-edit.md) |
| MCP resources | [list-mcp-resources.md](./list-mcp-resources.md), [read-mcp-resource.md](./read-mcp-resource.md) |

### P3 — Optional enhancements

[structured-logging.md](./structured-logging.md), [memdir.md](./memdir.md), [session-memory.md](./session-memory.md), [memory-extraction.md](./memory-extraction.md), [web-fetch.md](./web-fetch.md), [web-search.md](./web-search.md), [lsp.md](./lsp.md), [skill.md](./skill.md), [sleep-await.md](./sleep-await.md)

### P4 — Multi-agent / orchestration

[subagent-types.md](./subagent-types.md), [task-subagent.md](./task-subagent.md), [agent-resume-fork.md](./agent-resume-fork.md), [background-tasks.md](./background-tasks.md), [skills-framework.md](./skills-framework.md), [structured-sdk-output.md](./structured-sdk-output.md), [swarm.md](./swarm.md), task/todo/worktree tools — see [implementation-plan.md](./implementation-plan.md)
