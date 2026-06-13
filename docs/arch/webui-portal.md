---
title: WebUI Portal
slug: webui-portal
priority: P1
status: in-progress
spec: complete
code: in-progress
package: internal/portal
depends_on:
  - agent-loop
  - session-persistence
  - sse-streaming
---

# WebUI Portal

## Overview

The Jenny WebUI Portal is a lightweight, out-of-band "Command Center" for the Jenny CLI. It provides a visual interface for monitoring agent activity, managing sessions, configuring projects, and installing extensions (Skills/MCPs). 

The Portal adheres to the principle of "Observation over Interaction," maintaining the pure headless nature of the core agent while providing the rich visual bandwidth needed for professional workflows.

## Architecture

The Portal operates as a **Sidecar Observer** that interacts with the filesystem and process table without modifying the core agent logic.

```
[ Browser (WebUI) ] <--- SSE / HTTP ---> [ Jenny Portal Server ]
                                               |
                                     +---------+---------+
                                     |                   |
                              [ Filesystem ]      [ Process Table ]
                            ~/.jenny/sessions/     (fork/kill/signal)
                                     |                   |
                              [ Jenny Agent ] <----------+
                               (Headless)
```

## Core Principles

### 1. Sidecar Observer Model
The Portal is decoupled from the agent. It derives state by observing the filesystem and the system's process table.
- **No Database:** To maintain a small binary footprint, the Portal uses the existing `~/.jenny/sessions` structure.
- **UUID v7 Indexing:** Sessions are identified by UUID v7, which embeds a timestamp. The Portal lists sessions in chronological order by simply reading the directory names, avoiding the need for a separate index or database.
- **Liveness Detection:** The Portal determines if a session is "Running" or "Exited" by reading the `pid` file in the session directory and checking the process status (e.g., via `os.FindProcess(pid).Signal(0)` on Unix).

### 2. Headless Interaction Flow
Jenny is a headless CLI. The Portal manages this through detached process orchestration:
- **Starting Sessions:** The Portal spawns a new detached process: `jenny -p "prompt" --output-format stream-json`.
- **Real-time Observation:** The Portal uses `fsnotify` to monitor the session's transcript file. New events are streamed to the WebUI via **Server-Sent Events (SSE)**.
- **Resuming Sessions:** Resuming involves forking a new process with the `-r` flag: `jenny -r <id> -p "new prompt"`.

## UI Structure & Layout

The UI uses a **Master-Detail** layout, optimized for high-density information display.

### 1. Dashboard (Start)
The landing page for launching new work.
- **Global Stats:** Cards showing total sessions, active count, total token cost, and cache hit rates.
- **Prompt Input:** A large, focused area for entering the initial agent prompt.
- **Context Selectors:** Quick pickers for the Working Directory, Model Profiles, and global configurations.
- **Recent Projects:** A grid of frequently used project paths.

### 2. Sessions (Master-Detail)
The primary monitoring interface, inspired by advanced log viewers.
- **Master Sidebar:** A scrollable list of sessions (UUID v7 sorted), showing status indicators and brief activity summaries.
- **Detail Panel:**
  - **Metrics Row:** Real-time cost, token usage, turn count, and model name.
  - **Event Timeline:** A sequence of high-level events (Init, User Prompt, Tool Use, Result).
  - **Stream View:** A tail-style view of the raw transcript/stdout, rendered with Markdown and code highlighting.
  - **Control Bar:** Actions to "Stop" (SIGTERM), "Delete", or "Resume" (append prompt).

### 3. Projects View
Focuses on the long-term health and configuration of specific codebases.
- **Path Grouping:** Automatically groups sessions by their working directory (`cwd`).
- **Instruction Management:** Inline editor for project-level instructions like `AGENTS.md` or `CLAUDE.md`.
- **Project Assets:** View of Skills and MCPs active specifically for the selected project.

### 4. Marketplace & Assets
Management interface for the Jenny ecosystem.
- **Installed Assets:** Tabs for globally installed Skills, MCP servers, and Plugins.
- **Marketplace:** Interface to browse and install extensions from remote sources (e.g., GitHub).
- **Security:** Installation previews showing required permissions and README content.

## Technical Specifications

### Server Lifecycle
- **Single Instance:** The Portal uses `~/.jenny/portal.lock` to ensure only one server runs. 
- **Lockfile Schema:** A JSON file containing `{ "pid": int, "port": int, "auth_token": "string" }`.
- **Auto-Exit:** The server implements an ephemeral lifetime. If no browser heartbeats (HTTP pings) are received for 10 minutes, the server deletes the lockfile and exits.
- **Port Selection:** The server attempts to bind to a random high-range port (e.g., starting at 33669).

### Communication
- **SSE (Server-Sent Events):** Used for uni-directional streaming of session logs and status updates.
- **HTTP/JSON:** Used for control actions (Start, Resume, Kill) and fetching historical data.
- **I18n:** The frontend supports internationalization with `en` and `zh-Hans` locales pre-configured.

### Security
- **Localhost Only:** The server binds strictly to `127.0.0.1`.
- **Auth Token:** Every request from the WebUI must include the `auth_token` found in the lockfile (passed via URL parameter on initial load).

## Deployment

The WebUI is built as a React Single Page Application (SPA) using Vite. The production build is embedded into the Go executable using `//go:embed`.

```go
// internal/portal/server.go
//go:embed webui/dist/*
var webuiDist embed.FS
```

This ensures `jenny` remains a single-binary distribution.
