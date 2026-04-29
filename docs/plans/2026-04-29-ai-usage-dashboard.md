# AI Usage Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an MVP web dashboard that runs continuously in Docker and tracks local AI token usage from read-only log/session files.

**Architecture:** A Go HTTP service owns collection, storage, and UI delivery. Collector scans configured paths, parses tolerant token usage records, deduplicates by event hash, and writes to SQLite. Static HTML/CSS/JS renders summary metrics, daily usage, tool efficiency, recent events, and scan health.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, static HTML/CSS/JS, Docker Compose.

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`
- Create: `internal/config/config.go`

**Steps:**
1. Initialize Go module.
2. Add JSON config loader with validation.
3. Verify with `go test ./...`.

### Task 2: Storage And Collector

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/collector/collector.go`
- Create: `internal/collector/parser.go`
- Test: `internal/store/store_test.go`
- Test: `internal/collector/parser_test.go`

**Steps:**
1. Create SQLite schema for tools, events, and scan runs.
2. Implement idempotent event insertion with unique event hash.
3. Implement scanner over configured glob paths.
4. Implement tolerant JSONL/text token parser.
5. Verify parser and dedupe behavior with tests.

### Task 3: Web UI

**Files:**
- Create: `web/static/index.html`
- Create: `web/static/styles.css`
- Create: `web/static/app.js`

**Steps:**
1. Build data-dense dashboard layout.
2. Add KPI totals, daily chart, tool quota meters, recent events, and scan status.
3. Add accessible focus states and responsive layout.
4. Verify in browser.

### Task 4: Runtime Packaging

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `config.example.json`
- Create: `README.md`

**Steps:**
1. Build multi-stage Docker image.
2. Mount data directory writable and source logs read-only.
3. Document local and Docker execution.
4. Verify with `docker compose up --build`.

