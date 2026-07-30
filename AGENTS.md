# AGENTS.md

This repository should stay small, direct, and idiomatic.
Follow [Effective Go](https://go.dev/doc/effective_go) as the default style guide.

Write Go that feels like Go, not a translation from Java, C++, or a framework-heavy ecosystem.
When in doubt, choose the simpler shape with less indirection.
Keep this file focused on repo-specific guidance and non-obvious constraints rather than generic Go advice.

## Product Direction

Martie is no longer just a Telegram bot. Treat it as a small ptchan community
automation service with multiple surfaces:

- Telegram-facing announcements and discussion replies.
- ptchan-gateway-facing event consumption, thread reads, and eventually public
  ptchan replies.
- Stream monitoring for community broadcasts.

The current large trajectory is a ptchan-native assistant: Martie notices a
configured mention on ptchan, reads sanitized thread data from the gateway, asks
the model, and replies back through ptchan-gateway's posting API. Build around
that shape without making `chatter` the conceptual center of the
app.

ptchan-gateway is a useful architectural reference, especially its explicit
integration capabilities, signed API boundary, rate limits, public contract
types, and origin tracking. Do not copy its Rust structure into Go. Martie should
keep the more mature, stricter Go style in this repository: small packages,
plain data flow, explicit dependencies, and abstractions only where real callers
need them.

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
- Prefer plain functions over unnecessary methods, but if a dependency-holding struct helps, give it a concrete name like `threadnotifier` or `poller` instead of a generic `service`.
- Inline one-caller helpers and one-use temporaries when the code stays easy to read.
- If an expression reads poorly inline, prefer improving the helper name or shape over introducing a throwaway variable.
- Prefer early returns and explicit local error handling.
- Prefer plain helper names that describe the action, especially for transforms like `split...` or `lowercase...`.
- Prefer comments only for non-obvious decisions, constraints, or invariants; do not restate the code.
- Keep structs limited to fields the app actually uses.
- Prefer explicit field assignment over generic mapping layers.
- Do not write user-specific absolute paths into repo files or docs; prefer repo-relative paths or neutral placeholders.

## Package Boundaries

- `internal/app` owns process orchestration, config loading/validation, metrics
  registration, and gateway-event server plumbing. It should not own app
  behavior.
- `internal/apps/chatter` owns the Telegram discussion assistant.
- `internal/apps/channer` owns the ptchan public mention/reply responder.
- `internal/apps/threadnotifier` owns ptchan thread notification behavior, including
  notification filtering policy.
- `internal/apps/streamnotifier` owns stream polling behavior.
- Each `internal/apps/<app>/state` package owns that app's tables, migrations,
  records, and retention policy.
- `internal/storage` owns SQLite connection setup and low-level schema helpers
  only. It must not grow app domain behavior.
- `internal/telegram` owns Telegram Bot API transport, request/response DTOs,
  and generic message payload helpers. App-specific Telegram rendering belongs
  to the app that sends the message.
- `internal/gateway` owns signed ptchan-gateway webhook, thread reading, posting
  API payloads, and the gateway's normalized thread/post data. Martie should
  not fetch ptchan directly.
- App-specific ptchan policy should live with the app that uses it. Assistant
  text-to-thread-link detection belongs with assistant context because it is an
  input-enrichment concern, not a gateway protocol concern.
- `internal/assistant` owns shared prompt text helpers and ptchan context packet selection/rendering.
- `internal/deepseek` owns completion API transport and payloads.
- `internal/apps/streamnotifier/probe` owns stream probing and channel payloads
  because it is private to `streamnotifier`.
- `internal/localization` owns user-visible translations, not logs or config errors.
- Keep translation between external payloads and stored records inside the app
  that owns the workflow, not in `internal/storage`.
- Keep Telegram-specific admission, rendering, and memory behavior out of
  ptchan-native assistant code. Shared ptchan context rendering already lives
  outside `app`; model completion, rate limiting, signed
  gateway clients, and idempotency ledgers may be extracted when there is a real
  second consumer.

## Application Shape

- The binary has explicit app roles: `chatter`, `channer`,
  `threadnotifier`, and `streamnotifier`. Do not add aggregate convenience roles unless there
  is a stronger operational reason than local convenience.
- Treat the command as the application boundary. Configuration should not expose
  runtime component lists or aggregate modes.
- ptchan-gateway is Martie's ptchan boundary. New ptchan behavior should come
  through signed gateway webhooks, signed gateway thread reads, or signed
  gateway posting requests.
- Signed gateway webhooks are received by event-server plumbing in `app`, then
  dispatched to the selected app's consumers. Separate deployed app roles should
  use separate ptchan-gateway integrations when they need different webhook
  secrets, permissions, or metrics.
- A component failure must not stop unrelated components. The metrics server is process-level and may stop the process if it fails.
- `chatter` owns Telegram update orchestration, admission, rate
  limiting, completion, and delivery.
- `channer` should own ptchan mention admission, idempotent event
  processing, completion, and posting. It should not reuse Telegram update
  types or conversation assumptions.
- Keep channer side effects behind its SQLite event ledger. Persist only
  admitted work before model calls or posting, treat repeated admitted gateway
  deliveries as duplicates, and advance the existing row as processing
  continues. Ignored events should stay transient.
- `channer` is intentionally one-shot. Do not add automatic retries
  for completion or posting failures without revisiting the public duplicate-post
  risk. Final failures and unknown posting outcomes should be recorded and
  acknowledged so repeated webhook deliveries do not repeat public side effects.
- `conversation` owns temporary participant aliases, reply context, bounded in-memory history, and expiration. Conversation history is intentionally not persisted.
- Shared ptchan context should stay request-scoped. Do not persist fetched
  gateway thread data or model prompt packets.

## Configuration

- TOML contains application settings; environment variables are reserved for secrets and deployment paths.
- Keep TOML decoding strict. Unknown fields, duplicate keys, and malformed
  configured values should fail clearly.
- `LoadConfig` parses the document and converts settings for the selected app,
  including its ptchan integration identity. All roles use
  `storage.sqlite_path`.
  `ValidateRun` enforces that app's external dependencies. Do not make unrelated
  app settings require secrets, IDs, or semantically valid values.
- Keep one human-facing TOML file, but let app packages own their runtime config
  structs. `internal/app` may keep raw file-only structs for strict decoding,
  defaults, secret loading, app selection, and cross-cutting validation.
- Telegram-backed apps require `TELEGRAM_BOT_TOKEN`. `threadnotifier` and `streamnotifier`
  require the notification chat. `chatter` requires the
  discussion chat, access policy, DeepSeek credentials, and the ptchan
  integration secret. `channer` requires DeepSeek credentials and the ptchan
  integration secret, but not Telegram.
- Gateway webhooks, gateway thread reads, and gateway posting use
  `PTCHAN_INTEGRATION_<INTEGRATION_NAME>_SECRET`. `ptchan.chatter`,
  `ptchan.channer`, and `ptchan.threadnotifier` may override the top-level
  integration name for split app deployments.
- Prefer adding configuration only for meaningful deployment policy. Keep protocol safeguards and speculative tuning knobs in code.
- Keep `config/example.toml` as the complete configuration reference; keep README focused on human setup and operation.
- Prefer one SQLite database while data is small, operationally co-owned, or
  queried together. A separate database is acceptable when a component has a
  distinct lifecycle, retention policy, write volume, privacy boundary, or
  deploy path. Do not make normal workflows query multiple databases without a
  concrete reason.
- Applications log to stdout. Docker uses bounded local logs by default and can route them to persistent journald; do not add application-managed log files without a stronger requirement.
- Prefer a shared user-defined Docker network for container-to-container metrics scraping. Keep host port publication optional for host-based or external Prometheus deployments.
- Metrics are public operational contracts. Keep names component/surface-aware,
  labels low-cardinality, and README's metrics catalog in sync with any rename
  or semantic change. Never label metrics with user IDs, message text, prompts,
  thread/post IDs, gateway signatures, or other high-cardinality/private data.
- Keep system prompts surface-specific. `chatter.system_prompt` and
  `channer.system_prompt` should be separate because Telegram and
  public ptchan replies have different identity, privacy, and tone constraints.
- Keep ptchan mentions configurable and case-insensitive. The default mention is
  `@martie`.

## Refactoring Bias

- Remove abstractions before adding new ones.
- If two code paths differ only slightly, first ask whether one should disappear.
- If a package only wraps another package without adding meaning, simplify it.
- If code feels like a builder, manager, provider, or factory, stop and ask whether plain Go code would be clearer.
- It is acceptable to extract shared assistant components during the ptchan
  assistant refactor, but only around stable responsibilities with multiple real
  consumers: completion, rate limiting, signed gateway clients, or idempotency
  ledgers.
- Do not create a generic "assistant framework" until both surfaces force the
  same shape. Telegram and ptchan have different admission, identity, reply, and
  persistence rules.

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
