---
title: Git Helpers
slug: git-helpers
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/git
gaps:
  []
depends_on:
  []
---
# Git Helpers

## Overview

Filesystem-based git introspection without spawning `git` for hot paths: repo root, worktrees, ref safety, shallow detection, cached branch/HEAD/remote.

## findGitRoot(startPath)

Walk up from startPath seeking `.git` as directory **or** file (worktree/submodule).

- Memoized LRU cache (max 50 entries).
- Returns normalized path (NFC) or null.

## .git File vs Directory

| Type | Resolution |
|------|------------|
| Directory | Regular repo; git dir = `{root}/.git` |
| File | Parse `gitdir: <path>`; resolve relative to repo root |

`resolveGitDir(startPath)`: memoized per cwd.

## Worktree commondir Validation

Security checks for malicious `.git` / `commondir`:

1. `worktreeGitDir` parent must be `{commonDir}/worktrees`
2. `{worktreeGitDir}/gitdir` realpath must equal `{realpath(gitRoot)}/.git`
3. Reject otherwise; fall back to input root

## Shallow Clone

`isShallowClone()`: true iff `{commonDir}/shallow` exists.

## Branch / HEAD / Remote Cache

`GitFileWatcher`: watch HEAD, config, current branch ref (1s interval).

Worktree: branch refs + config from `commonDir`.

Cached: `getCachedBranch()`, `getCachedHead()`, `getCachedRemoteUrl()`, `getCachedDefaultBranch()`.

Invalidate on any watched file change.

## Ref Safety

Reject path traversal (`..`), leading `-`, shell metacharacters in refs/SHAs from `.git/`.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Detached HEAD | Raw hex SHA only |
| Symref chains | Follow in loose refs and packed-refs |
| Submodule .git file | Separate repo if no commondir |
| Bare repo worktree | Canonical root may be common dir |

## Acceptance Criteria

- **AC1:** findGitRoot cached and consistent for nested paths.
- **AC2:** Valid worktree resolves refs from common dir.
- **AC3:** Malicious commondir rejected.
- **AC4:** isShallowClone detects shallow file.
- **AC5:** Cache invalidates on ref change.
