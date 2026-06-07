---
title: Skill Tool
slug: skill
priority: P3
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  - Runtime enforcement of `allowed-tools` patterns.
  - Tracking "Active Skills" across context compactions in the system prompt.
depends_on:
  - tool-registry
  - task-subagent
  - system-prompt
---
# Skill Tool

## Overview

Invokes slash-command skills by name. Skills are portable units of specialized knowledge, instructions, and resources following the [Agent Skills](https://agentskills.io) specification.

## Standard Specification

A skill is a directory containing a mandatory `SKILL.md` file and optional subdirectories.

### Directory Structure
```text
skill-name/
├── SKILL.md       # Required: Metadata + Instructions
├── scripts/       # Optional: Executable code (bash, python, etc.)
├── references/    # Optional: Documentation (PDFs, MD, JSON)
└── assets/        # Optional: Templates, images, or static resources
```

## Behavior & Implementation

### 1. Discovery & Manifest
Skills are discovered at startup from project, user, and bundled directories.
- **System Prompt Manifest:** The `ToolRegistry` populates a "Skills Manifest" section in the system prompt (`system-prompt.md`).
- **Manifest Content:** For each skill, only `name` and `description` are included. This follows the ~100 token "Discovery" requirement.
- **Example Manifest:**
  ```text
  Available Skills:
  - readme-writer: Creates professional README.md files. Use when...
  - deploy-helper: Assists with CI/CD deployment. Use when...
  ```

### 2. Activation Tool (`ActivateSkill`)
The agent invokes `ActivateSkill(name: string)` to load a skill.

**Response Schema:**
```json
{
  "name": "skill-name",
  "root_path": "/absolute/path/to/skill",
  "content": "... full SKILL.md text ..."
}
```
The response is also rendered to the agent wrapped in `<activated_skill>` tags including the `root_path` attribute.

### 3. Resource Access & Path Resolution
Skills use relative paths (e.g., `scripts/deploy.sh`).
- **Resolution:** The agent MUST combine the `root_path` with the relative path to use system tools like `Read` or `Bash`.
- **Environment:** When running `scripts/`, the `SKILL_ROOT` environment variable should be set to the skill's root path.

### 4. Tracking Active Skills
To prevent losing skill context during [Context Compaction](context-compaction.md):
- The system prompt should include an "Active Skills" section listing the names and root paths of all skills activated in the current session.
- **Compaction:** When turns are summarized, the summary should preserve the fact that specific skills were activated.

### 5. Heavy Skills & Subagents
"Heavy" skills (complex multi-step tasks) should be forked using the `Task` tool.
- The `Task` tool prompt should include the skill's instructions.
- The `subagent_type` is chosen by the agent (e.g., `explore`, `shell`).

### 6. Security (`allowed-tools`)
- **Format:** Space-separated list of tool patterns (e.g., `Read`, `Bash(git:*)`, `Glob`).
- **Enforcement:** 
  - **Soft:** Included in the skill instructions for the agent to follow.
  - **Hard (Optional):** If a subagent is spawned, the `Task` tool should filter the toolset based on this list.

## Acceptance Criteria

- **AC1:** `ActivateSkill` returns `root_path` and content.
- **AC2:** System prompt contains a manifest of available skill names and descriptions.
- **AC3:** System prompt tracks "Active Skills" (name + path) across the session.
- **AC4:** Relative paths in skills are resolvable via `root_path`.
- **AC5:** `SKILL_ROOT` env var is set when executing skill scripts.
- **AC6:** Unknown skill names return a clear error.
