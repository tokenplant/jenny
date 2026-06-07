---
title: LSP Tool
slug: lsp
priority: P3
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# LSP Tool

## Overview

Language Server Protocol operations for code intelligence. Read-only, concurrency-safe.

## Gating

Requires LSP server connected (`ENABLE_LSP_TOOL`). Fail clearly if not connected.

## Limits

Max file size: **10 MB**.

Coordinates: **1-based** line and character (editor style); convert to LSP 0-based internally.

## Operations

goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls.

Wait for pending LSP init before operations.

Filter gitignored locations from results.

## Acceptance Criteria

- **AC1:** 1-based coordinates in tool API.
- **AC2:** Clear error when LSP disconnected.
- **AC3:** Files >10MB rejected.
- **AC4:** Concurrency-safe read-only.
- **AC5:** Gitignored paths filtered from results.
