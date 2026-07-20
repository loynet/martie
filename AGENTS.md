# AGENTS.md

This repository should stay small, direct, and idiomatic.
Follow [Effective Go](https://go.dev/doc/effective_go) as the default style guide.

Write Go that feels like Go, not a translation from Java, C++, or a framework-heavy ecosystem.
When in doubt, choose the simpler shape with less indirection.
Keep this file focused on repo-specific guidance and non-obvious constraints rather than generic Go advice.

## For Agents

- Optimize for readability by the next person opening the file.
- Prefer small, concrete changes over broad rewrites.
- Keep the main flow visible and easy to scan from top to bottom.
- If a rule here conflicts with code that is already clearer and idiomatic, follow the clearer code.

## Repo Preferences

- Prefer simple, concrete code over clever abstractions.
- Prefer familiar Go patterns when they make the code easier for an experienced Go developer to scan and navigate.
- Do not add constructor-style `New...` functions or builder patterns that only copy fields; use `New...` only when it validates input, sets defaults, or wires dependencies.
- Do not invent interfaces until they are needed by real consumers.
- Prefer plain functions over unnecessary methods, but if a dependency-holding struct helps, give it a concrete name like `bot` instead of a generic `service`.
- Inline one-caller helpers and one-use temporaries when the code stays easy to read.
- If an expression reads poorly inline, prefer improving the helper name or shape over introducing a throwaway variable.
- Prefer early returns and explicit local error handling.
- Prefer plain helper names that describe the action, especially for transforms like `split...` or `lowercase...`.
- Prefer comments only for non-obvious decisions, constraints, or invariants; do not restate the code.
- Keep structs limited to fields the app actually uses.
- Prefer explicit field assignment over generic mapping layers.
- Do not write user-specific absolute paths into repo files or docs; prefer repo-relative paths or neutral placeholders.

## Package Boundaries

- `internal/app` owns orchestration and application rules.
- `internal/telegram` owns Telegram-specific rendering and delivery.
- `internal/gateway` owns signed ptchan-gateway webhook and context API payloads.
- `internal/ptchan` owns ptchan-facing domain types and link/filter helpers. Martie should not fetch ptchan directly.
- `internal/deepseek` owns completion API transport and payloads.
- `internal/miau` owns stream probing and channel payloads.
- `internal/localization` owns user-visible translations, not logs or config errors.
- `internal/state` owns persistence.
- Keep translation between external payloads and stored records in `app`, not in `state`.

## Application Shape

- The runtime has three independently selectable components: `gateway`, `streams`, and `assistant`.
- Keep component selection explicit in `runtime.components`; do not infer enablement from empty configuration such as an empty stream list.
- ptchan-gateway is Martie's ptchan boundary. New ptchan behavior should come through signed gateway webhooks or signed gateway context requests.
- A component failure must not stop unrelated components. The metrics server is process-level and may stop the process if it fails.
- `assistant` owns Telegram update orchestration, admission, rate limiting, completion, and delivery.
- `conversation` owns temporary participant aliases, reply context, bounded in-memory history, and expiration. Conversation history is intentionally not persisted.

## Configuration

- TOML contains application settings; environment variables are reserved for secrets and deployment paths.
- Keep TOML decoding strict. Unknown fields, unknown components, duplicates, and malformed configured values should fail clearly.
- `LoadConfig` parses the document. `ValidateRun` enforces dependencies of selected components. Do not make disabled components require unrelated secrets or IDs.
- All running components currently require Telegram. Only gateway and streams require the notification chat; only assistant requires the discussion chat, access policy, and DeepSeek credentials.
- Gateway webhooks and assistant ptchan context use `PTCHAN_WEBHOOK_<CONSUMER_NAME>_SECRET`.
- Prefer adding configuration only for meaningful deployment policy. Keep protocol safeguards and speculative tuning knobs in code.
- Keep `config.example.toml` as the complete configuration reference; keep README focused on human setup and operation.
- Applications log to stdout. Docker uses bounded local logs by default and can route them to persistent journald; do not add application-managed log files without a stronger requirement.
- Prefer a shared user-defined Docker network for container-to-container metrics scraping. Keep host port publication optional for host-based or external Prometheus deployments.

## Refactoring Bias

- Remove abstractions before adding new ones.
- If two code paths differ only slightly, first ask whether one should disappear.
- If a package only wraps another package without adding meaning, simplify it.
- If code feels like a builder, manager, provider, or factory, stop and ask whether plain Go code would be clearer.

## Operational Notes

- Keep the program's entrypoints easy to follow.
- Prefer package-level entrypoints when they read better than empty service constructors.
- Prefer the repo's `make` targets for common local workflows: `make fmt`, `make lint`, `make test`, `make check`, `make build`, `make run`, and `make clean`.
- Run `gofmt` on changed Go files.
- Validate with `go test ./...` and `go vet ./...` when checks are needed.
- If the environment blocks the default Go build cache, use the ignored repo-local cache with `GOCACHE="$PWD/.gocache"`.
- For thin HTTP clients, prefer transport-level test fakes over `httptest.NewServer` when sandboxing may block local listeners.
- Prefer tests for non-obvious logic, persistence behavior, state transitions, and protocol edge cases.
- Do not add tests for straightforward wiring or behavior that is already easy to verify by reading the code.
