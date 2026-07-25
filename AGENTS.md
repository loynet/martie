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

The next large trajectory is a ptchan-native assistant: Martie should notice a
configured mention on ptchan, read sanitized thread data from the gateway, ask the model, and
reply back through ptchan-gateway's posting API. Build toward that shape without
making the current Telegram assistant the conceptual center of the app.

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
- Prefer plain functions over unnecessary methods, but if a dependency-holding struct helps, give it a concrete name like `notifier` or `poller` instead of a generic `service`.
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
- `internal/gateway` owns signed ptchan-gateway webhook, thread reading, and posting API payloads.
- `internal/ptchan` owns ptchan-facing domain types and link/filter helpers. Martie should not fetch ptchan directly.
- `internal/assistant` owns assistant-side prompt traces, shared prompt text helpers, and ptchan context packet selection/rendering.
- `internal/deepseek` owns completion API transport and payloads.
- `internal/miau` owns stream probing and channel payloads.
- `internal/localization` owns user-visible translations, not logs or config errors.
- `internal/state` owns persistence. Keep persistence files grouped by domain
  even when they share one SQLite database.
- Keep translation between external payloads and stored records in `app`, not in `state`.
- Keep Telegram-specific admission, rendering, and memory behavior out of future
  ptchan-native assistant code. Shared prompt tracing and ptchan context
  rendering already live outside `app`; model completion, rate limiting, and
  retry queues may be extracted when there is a real second consumer.

## Application Shape

- The runtime has independently selectable components: `gateway`, `streams`,
  `telegram_assistant`, and `ptchan_assistant`.
- Keep component selection explicit in `runtime.components`; do not infer enablement from empty configuration such as an empty stream list.
- ptchan-gateway is Martie's ptchan boundary. New ptchan behavior should come
  through signed gateway webhooks, signed gateway thread reads, or signed
  gateway posting requests.
- Signed gateway webhooks are received by shared event-server plumbing in
  `app`, then dispatched to enabled consumers. `gateway` is the Telegram thread
  notification consumer; it is not the only owner of ptchan events.
- A component failure must not stop unrelated components. The metrics server is process-level and may stop the process if it fails.
- `telegram_assistant` owns Telegram update orchestration, admission, rate
  limiting, completion, and delivery.
- `ptchan_assistant` should own ptchan mention admission, idempotent event
  processing, completion, and posting. It should not reuse Telegram update
  types or conversation assumptions.
- `conversation` owns temporary participant aliases, reply context, bounded in-memory history, and expiration. Conversation history is intentionally not persisted.
- Shared ptchan context should stay request-scoped. Do not persist fetched
  gateway thread data or model prompt packets unless an explicit trace setting is
  enabled.

## Configuration

- TOML contains application settings; environment variables are reserved for secrets and deployment paths.
- Keep TOML decoding strict. Unknown fields, unknown components, duplicates, and malformed configured values should fail clearly.
- `LoadConfig` parses the document. `ValidateRun` enforces dependencies of selected components. Do not make disabled components require unrelated secrets or IDs.
- Telegram-backed components require `TELEGRAM_BOT_TOKEN`. `gateway` and
  `streams` require the notification chat. `telegram_assistant` requires the
  discussion chat, access policy, and DeepSeek credentials. `ptchan_assistant`
  should require DeepSeek credentials and the ptchan integration secret, but not
  Telegram.
- Gateway webhooks, gateway thread reads, and gateway posting use
  `PTCHAN_INTEGRATION_<INTEGRATION_NAME>_SECRET`.
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
- Keep system prompts surface-specific. `telegram_assistant.system_prompt` and
  `ptchan_assistant.system_prompt` should be separate because Telegram and
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
  consumers: completion, rate limiting, retry queues, or signed gateway clients.
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
