# Crocodile Implementation Plan

> Execution: inline using executing-plans; user authorised implementation of the agreed design.

**Goal:** Play a complete private drawing telephone game across separate devices.

**Architecture:** Isolated Go game package, atomic bbolt transactions, silverlining HTTP adapter on `/api/game/*`. Vite/TypeScript frontend polls personal state and uses Canvas. Existing handlers and Drive storage keep their contracts.

**Tech Stack:** Existing Go 1.25.8, bbolt, silverlining; TypeScript and Vite; browser Canvas/Pointer Events.

## Global constraints

- Preserve existing API responses, authorisation and data.
- 3–12 players; everyone draws every topic, 2N stages, no timer.
- Personal assignments stay private until results; credentials never travel in invitation links.
- Same frontend works on phones and computers; one shared backend.

## Task 1: Persistent game and HTTP API

Files: `internal/game/game.go`, `internal/game/http.go`, `internal/game/game_test.go`, `internal/http/game.go`; additive integration in `internal/http/routes.go` and `cmd/drawing-service/main.go`.

- [x] Write a real HTTP test: create rooms, join 3/4 players, ready, submit all stages, assert every chain has N unique drawing authors, no self-description, private state, final histories.
- [x] Run `go test ./internal/game` to observe missing implementation.
- [x] Implement persisted room commands, random guest credentials, limits, PNG validation, atomic stage advancement, per-user projection, restart/leave/expiry. New buckets only.
- [x] Run `go test -race ./...`; include concurrent last submissions, duplicate/stale commands, persistence and unauthorised image access.

API: `POST /api/game/rooms {name}`, `POST /api/game/rooms/{code}/join {name}` return `{token, room}`. Authenticated `GET /api/game/rooms/{code}` returns personal room state. `POST .../{ready,submit,restart,leave}` returns state (`leave`: 204). Submission includes `{gameId, stage, text?, image?}`; image is PNG base64. `GET .../images/{imageId}` is authenticated and checked against current assignment/results. Bearer token header for all authenticated calls.

## Task 2: Frontend and runnable game server

Files in `/Users/evgeny/Work/web-crocodile`: `package.json`, `tsconfig.json`, `vite.config.js`, `index.html`, `src/{main,types,canvas}.ts`, `src/style.css`, `.gitignore`, README. API client stays in main.ts. Backend `cmd/crocodile/main.go` allows local gameplay without Google credentials on a separate database.

- [x] Implement typed API client with bounded requests and sequential polling; no secret in URLs.
- [x] Implement join/create, lobby, text, canvas, waiting and full-chain results views. Preserve drafts during polling and reload, report network failures without losing work.
- [x] Add pointer drawing, undo/erase/clear, bounded local history and responsive canvas. Clear successfully submitted drafts; abandoned drafts survive locally for recovery.
- [x] Run `npm run build` (TypeScript + Vite). Run game server and frontend on LAN, document same-origin reverse proxy for hosting.

## Task 3: Verify complete flow

- [x] Run browser test with independent sessions through 6 stages, reload a draft, check image visibility and all results, restart room. Check mobile viewport and touch drawing.
- [x] Run backend race tests, frontend build and inspect final diffs for preservation of existing methods.
- [x] Document exact commands, capacity/expiry limits, shared server requirement and verification results.

## Verification on 2026-09-06

- `go test -race ./... -timeout 60s`: 55 passed across 10 packages. Legacy HTTP test environment now also enables the game routes.
- `npm run build`: strict TypeScript and Vite succeeded.
- Real browser check: all 6 stages, 3 separate browser contexts, touch events, text/canvas reload, undo, final PNGs, HTML escaping, host leave/transfer, new game and invalid-session recovery passed; zero page errors.
- Independent code review: no remaining blockers after fixing finished-player metadata and refreshed host controls.
- Regression for PNGs exceeding 8 KiB caught silverlining 1.3.3 buffer reuse/direct-read accounting bugs. Game HTTP adapter clones authorization and uses fixed 4 KiB reads; legacy handlers are untouched.
- Local preview runs on port 5173, dev game API on 8091 with `/tmp/crocodile-dev-20260906.db`. Internet deployment has not been performed.
