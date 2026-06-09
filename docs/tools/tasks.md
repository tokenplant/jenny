---
title: Tasks
slug: tasks
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
  - task-create
  - background-tasks
  - task-subagent
  - subagent-types
  - agent-loop
---
# Tasks

Task-related tool specifications for the Todo v2 system and related background task handling.

## TodoWrite Tool

### Overview

In-session todo list for agent planning. Stored per agent/session key.

### Behavior

- All items `completed` → store cleared to `[]`
- Disabled when Todo v2 enabled (TaskCreate/Get/List/Update replace it)

### Acceptance Criteria

- **AC1:** Todos persist per session key.
- **AC2:** All completed clears list.
- **AC3:** Disabled when Todo v2 on.

## TaskCreate Tool

### Overview

Creates tracked tasks in Todo v2 system. Requires Todo v2 enabled.

### Parameters

subject, description, optional activeForm, metadata.

### Hooks

Hooks may block creation and roll back (delete task) on failure.

### Acceptance Criteria

- **AC1:** Only enabled with Todo v2.
- **AC2:** Hook failure rolls back task.

## TaskGet Tool

### Overview

Retrieves single task by ID.

### Behavior

Return `task: null` gracefully → tool result "Task not found" (not hard error).

### Acceptance Criteria

- **AC1:** Missing task returns null gracefully.
- **AC2:** Found task returns full record including `id`, `subject`, `description`, `active_form`, `status`, `created_at`, `updated_at`, `metadata`, `blocks`, and `blocked_by`.

## TaskList Tool

### Overview

Lists tasks with optional filters.

### Behavior

- Filter `_internal` metadata from output
- Output includes `blocks` and `blocked_by` arrays for each task
- Strip resolved blockers from `blocked_by` arrays

### Acceptance Criteria

- **AC1:** Internal metadata not exposed.
- **AC2:** Resolved blockers stripped from blockedBy.

## TaskUpdate Tool

### Overview

Updates task fields, status, and dependency graph.

### Parameters

| Param | Description |
|-------|-------------|
| `task_id` | Required task identifier |
| `subject` | New subject |
| `description` | New description |
| `active_form` | New active form |
| `status` | New status (`pending`, `in_progress`, `completed`, `deleted`) |
| `metadata` | Metadata object (merge on update, null value deletes key) |
| `add_blocks` | Array of task IDs this task blocks |
| `add_blocked_by` | Array of task IDs this task is blocked by |

### Output

Returns updated task with `blocks` and `blocked_by` arrays.

### Acceptance Criteria

- **AC1:** deleted status removes or marks task deleted.
- **AC2:** addBlocks/addBlockedBy update graph.
- **AC3:** null metadata key deletes field.

## TaskStop Tool

### Overview

Stops running background tasks.

### Rules

- Only `running` tasks stoppable
- Accepts deprecated `shell_id` alias for task ID

### Acceptance Criteria

- **AC1:** Non-running task errors clearly.
- **AC2:** shell_id alias accepted.

## TaskOutput Tool

### Overview

Retrieves output from background tasks/agents with optional blocking wait.

### Parameters

| Param | Default |
|-------|---------|
| `block` | true |
| Poll interval | 100ms |
| `timeout` | 30s (max 600s) |

Prefer in-memory agent result over raw transcript JSONL.

### Acceptance Criteria

- **AC1:** block=true polls every 100ms.
- **AC2:** Default timeout 30s.
- **AC3:** Max timeout 600s.
- **AC4:** In-memory result preferred.

## TaskSubAgent Tool

### Overview

Spawns subagents with typed tool allowlists. Wire tool name `Agent` (legacy alias `Task`).

### Parameters

| Param | Description |
|-------|-------------|
| `description` | Short task label |
| `prompt` | Subagent instruction |
| `subagent_type` | Built-in type name |
| `model` | Optional model override |
| `run_in_background` | Async execution |
| `isolation` | `worktree` for temp worktree |
| `cwd` | Working directory override |

### Results

| Mode | Return |
|------|--------|
| Sync | Final text result |
| Async | `{ status: async_launched, agentId, outputFile }` |

Per-type tools via `resolveAgentTools`. Partial extraction on interrupt.

### Acceptance Criteria

- **AC1:** subagent_type selects allowlist.
- **AC2:** Sync returns text; async returns outputFile.
- **AC3:** worktree isolation creates temp worktree.
- **AC4:** Interrupt extracts partial result.
