# CLAUDE.md

This repository is a Go 1.26+ local AI proxy server. It exposes OpenAI-compatible APIs, routes requests across multiple upstream providers, manages OAuth/API-key credentials, tracks usage, and serves both a TUI and an embedded browser dashboard.

## Quick Commands

```bash
gofmt -w .
go build -o cli-proxy-api ./cmd/server
go build -o test-output ./cmd/server && rm test-output
go test ./...
go run ./cmd/server
go run ./cmd/server --tui
```

Common flags:
- `--config <path>`
- `--tui`
- `--standalone`
- `--local-model`
- `--no-browser`
- `--oauth-callback-port <port>`

Default config is `config.yaml`; template is `config.example.yaml`. `.env` is loaded from the working directory. Auth files default to `auths/`.

## Fork Sync Workflow

- Keep `main` as a clean mirror of `upstream/main`.
- Keep local/custom work on a separate branch (current branch: `fork/webui`).
- To update from upstream without losing local work:

```bash
git fetch upstream
git switch main
git reset --hard upstream/main
git switch fork/local-customizations
git rebase main
```

- If rebase conflicts, resolve them, `git add <files>`, then run `git rebase --continue`.
- Do not commit local customization work directly to `main`.

## Product Capabilities

- OpenAI-compatible API surface: `/v1/chat/completions`, `/v1/completions`, `/v1/models`
- Provider support for OpenAI-compatible, Gemini, Gemini CLI, Claude, Codex/OpenAI OAuth, Antigravity, Kimi, Vertex, and Amp-style routes
- OAuth login/token refresh flows for supported providers
- API key and auth-file based credential pools
- Request routing across credentials/providers
- Usage and token accounting
- Prompt/cache accounting helpers
- Request log and management visibility
- Codex WebSocket execution
- WebSocket relay sessions
- Remote model registry updates
- Config/auth hot reload
- Bubbletea TUI
- Embedded dashboard at `/dashboard`
- SDK entrypoint for embedding the proxy in other Go apps

## Main Runtime Flow

1. `cmd/server` loads config, env, stores, auth watchers, registry/model updater, usage/logging, and starts the API server.
2. `internal/api` registers OpenAI-compatible routes, provider modules, management routes, dashboard assets, and middleware.
3. `internal/translator/*` maps request/response protocols between OpenAI-compatible shapes and provider-specific shapes.
4. Credential/client selection resolves configured auth files and API keys.
5. `internal/runtime/executor/*` performs upstream calls and streaming/WebSocket execution.
6. `internal/thinking` applies thinking/reasoning behavior through a canonical config pipeline before provider-specific output.
7. `internal/usage`, `internal/cache`, `internal/watcher`, and management handlers update local operational state.

## Directory Map

- `cmd/server/` — Server CLI entrypoint, flags, lifecycle, TUI/server startup
- `cmd/fetch_antigravity_models/` — Antigravity model fetch utility
- `internal/api/` — Gin server, routes, middleware, management routes, embedded assets
- `internal/api/handlers/management/` — Dashboard bootstrap, auth files, usage, logs, settings/config, OAuth sessions, quotas
- `internal/api/modules/amp/` — Amp integration, fallback handlers, Gemini bridge, proxy/rewriter
- `internal/auth/` — OAuth/token flows for Claude, Codex/OpenAI, Gemini, Kimi, Antigravity, Vertex
- `internal/runtime/executor/` — Provider executors; helpers belong in `internal/runtime/executor/helps/`
- `internal/translator/` — Provider protocol translators and shared translator utilities
- `internal/thinking/` — Canonical thinking config pipeline; preserve canonical config → provider applier architecture
- `internal/registry/` — Model registry and remote updater
- `internal/config/` — Config structures/loading
- `internal/store/` — Storage implementations and secret resolution
- `internal/cache/` — Request signature and prompt-cache related helpers
- `internal/usage/` — Usage/token accounting
- `internal/watcher/` — Config/auth hot reload and diffs
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/tui/` — Bubbletea TUI
- `cmd/console/static/` — Primary browser dashboard/frontend assets and Node tests for the current UI
- `internal/dashboardasset/` — Legacy embedded dashboard assets; do not treat as the primary UI target unless a task explicitly says so
- `internal/managementasset/` — Management/config snapshot assets
- `sdk/cliproxy/` — Embeddable proxy service/builder/watchers/pipeline
- `sdk/*` — SDK-facing helpers/mirrors for auth, config, access, API, translation, logging
- `test/` — Cross-module integration tests
- `examples/` — Usage examples

## Dashboard Frontend Direction

The current primary browser UI lives under:

- `cmd/console/static/index.html`
- `cmd/console/static/app.js`
- `cmd/console/static/app.test.js`
- `cmd/console/static_tests/app.test.js`

Treat `cmd/console/static/*` as the default frontend target for dashboard and management UI work unless a task explicitly says to work on legacy assets.

`internal/dashboardasset/*` exists as legacy/alternate embedded dashboard code. Do not default new UI work there.

Important: the current `cmd/console/static/app.js` mixes API routes, payload handling, state, rendering, OAuth UI, filtering, settings wiring, and logs/account interactions. Do not make that coupling worse.

When touching the frontend, prefer small local helpers and narrow backend support over inventing fake UI state.

Rules for dashboard work:
- Keep visual redesign separate from behavior or architecture changes.
- Build the UI only around capabilities this project actually has.
- Do not invent controls, statuses, metrics, or workflows that are not backed by current project behavior.
- If the UI needs extra backend support, add the smallest management/API change that exposes real state.
- Do not render fake settings or fake statuses.
- Allowed account status vocabulary in the UI: `active`, `paused`, `disabled`, `rate_limited`, `deactivated`, `syncing`, `error`.
- Settings UI should render only controls backed by real management endpoints.
- If backend payload shapes change, update the frontend normalization/derivation path before tweaking rendering.

## High-Risk Areas

- `internal/translator/`: avoid standalone changes unless explicitly required; translator work affects provider compatibility.
- `internal/thinking/`: do not break the canonical representation → provider translation pipeline.
- `internal/runtime/executor/`: keep provider executors focused; shared helper files go under `helps/`.
- Credential/auth code: avoid logging secrets/tokens.
- Network timeouts: after upstream connection establishment, do not add arbitrary timeouts. Allowed exceptions are documented in `AGENTS.md`.

## Verification Expectations

After Go changes:

```bash
gofmt -w .
go build -o test-output ./cmd/server && rm test-output
```

After dashboard/frontend changes in the current UI:

```bash
node --test cmd/console/static/app.test.js
node --test cmd/console/static_tests/app.test.js
go build -o test-output ./cmd/server && rm test-output
```

Run narrower tests when editing a specific package, then run the build check before claiming completion.
