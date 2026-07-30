# martie

Martie is a small ptchan community automation service. The same binary can run
several app roles:

- `chatter` runs the Telegram discussion assistant.
- `channer` runs the ptchan-native public reply assistant.
- `threadnotifier` consumes signed ptchan-gateway events and announces eligible threads to Telegram.
- `streamnotifier` watches configured stream URLs and announces when they go live.

Martie uses long polling for Telegram where needed, stores its small amount of durable state in SQLite, receives private webhooks from ptchan-gateway, and can expose Prometheus metrics.

## Direction

Martie is moving from a Telegram-centered bot into a small, multi-surface
automation service for the ptchan community. Telegram remains important, but it
is one delivery and conversation surface rather than the whole product shape.

The ptchan-native assistant flow is:

1. ptchan-gateway observes a new ptchan post and sends Martie a signed webhook.
2. Martie admits the post only if it includes a configured mention.
3. Martie records admitted work before model calls or posting, so gateway
   retries do not duplicate public replies.
4. Martie reads sanitized thread data from ptchan-gateway.
5. Martie asks the configured model using a ptchan-specific system prompt and
   bounded context packet.
6. Martie replies through ptchan-gateway's signed posting API.

ptchan-gateway is the ptchan boundary. Martie should not fetch or post to ptchan
directly; new ptchan behavior should go through signed gateway APIs. The gateway
already models the useful pieces for this direction: explicit integration
capabilities, board-scoped access, reading and posting rate limits, origin
tracking for produced posts, and public sanitized contracts.

Martie receives signed gateway events at the stable
`/internal/ptchan/events` route. The integration webhook URL configured in
ptchan-gateway must use that path. Martie accepts webhook schema version `1`,
deduplicates deliveries by `event_id`, and safely acknowledges future v1 event
kinds that its selected app does not yet use.

The Go code should stay plain and strict while it grows. Shared pieces should be
extracted when there are real consumers, especially model completion, prompt
rendering, signed gateway clients, idempotency
ledgers, and rate limiting. Avoid turning the two assistants into a generic framework too
early: Telegram and ptchan have different identity, admission, privacy, and
reply semantics.

Persistence follows the same bias. Each Martie process uses the single SQLite
path configured by `storage.sqlite_path`.

## Code Shape

The source tree makes app ownership explicit:

```text
internal/apps/chatter        Telegram discussion assistant
internal/apps/channer        ptchan public mention/reply responder
internal/apps/threadnotifier threadnotifier app
internal/apps/streamnotifier streamnotifier app
```

Each app owns its behavior, runtime config shape, and, when needed, its own
`state` package with that app's tables, migrations, records, and retention
policy. Apps may share one physical SQLite file, but they should not casually
query each other's tables.

`internal/storage` is only the SQLite substrate. `internal/app` is process
orchestration: command-selected app wiring, metrics registration, config
validation, health endpoints, and signed gateway-event dispatch.
`internal/telegram` is only the Bot API transport and generic message
payloads; Telegram rendering for chatter, threadnotifier, and streamnotifier lives with those
apps.

## Run locally

Requirements: Go 1.25 or newer, a Telegram bot token when running Telegram-backed apps, and a DeepSeek API key when running assistant apps.

```bash
cp .env.example .env.dev
mkdir -p config
cp config/example.toml config/dev.toml
```

Edit both files, then run:

```bash
make run
```

Configuration is split deliberately:

- `.env.dev` contains secrets.
- `config/dev.toml` contains application settings.
- `MARTIE_APP` selects `chatter`, `channer`, `threadnotifier`, or `streamnotifier` for Makefile and Docker workflows.

The example TOML documents every setting. Copy it, then remove optional sections you do not want. Unknown keys and invalid values fail at startup. `MARTIE_ENV=prod` selects `.env.prod` and `config/prod.toml`. `BOT_ENV` is still accepted by the Makefile as an older alias.

Run one app explicitly:

```bash
make run MARTIE_APP=channer
make run MARTIE_APP=chatter
make run MARTIE_APP=threadnotifier
make run MARTIE_APP=streamnotifier
```

The binary accepts the same app names directly:

```bash
martie chatter
martie channer
martie threadnotifier
martie streamnotifier
```

Validate an environment locally before running it:

```bash
make check-config MARTIE_ENV=prod MARTIE_APP=channer
```

On first startup, the threadnotifier app records its bootstrap time and suppresses older webhook events. New events observed after that point are processed normally.

The gateway webhook listener starts when the selected app consumes ptchan
events, such as `channer` or `threadnotifier`. When those apps run
separately, use separate ptchan-gateway integrations and point their webhook
URLs at the corresponding Martie processes.

App dependencies are deliberately checked only for the selected app role.
For example, `channer` is designed to run without Telegram. The
`chatter`, `threadnotifier`, and `streamnotifier` apps need Telegram settings for
their Telegram-facing behavior.

## Deploy with Docker

Create `.env.prod` and `config/prod.toml`, then deploy:

```bash
make docker-deploy MARTIE_ENV=prod MARTIE_APP=channer
```

`docker-deploy` builds the image, validates the selected environment with
`martie check-config <app>` inside that image, then replaces the container only
after the check passes.

Useful operational commands:

```bash
make docker-logs MARTIE_ENV=prod
make docker-clean
```

The container runs as a non-root user with a read-only filesystem. The selected TOML file is mounted read-only, secrets are passed through the environment, and SQLite is stored in the persistent app volume.

Docker images are tagged with the current commit by default, for example
`martie:abc1234`. Override `IMAGE` when pushing to a registry or using a
specific tag:

```bash
make docker-deploy MARTIE_ENV=prod IMAGE=registry.example/martie:abc1234
```

`MARTIE_ENV` and `MARTIE_APP` select the environment-specific inputs and Docker resource names:

```text
MARTIE_ENV=dev  MARTIE_APP=channer -> .env.dev,  config/dev.toml,  martie-dev-channer,  martie-dev-channer-data
MARTIE_ENV=prod MARTIE_APP=threadnotifier  -> .env.prod, config/prod.toml, martie-prod-threadnotifier, martie-prod-threadnotifier-data
```

Inside the container, the selected config is mounted read-only at
`/etc/martie/config.toml`. Set `storage.sqlite_path = "data/martie.db"` in the
mounted config to store SQLite at `/data/martie.db` on the named Docker volume.
`docker-deploy` replaces the container but keeps the volume.

Different apps and environments can run on the same host at the same time. They
see the same SQLite path inside their containers, but that path is
backed by different named volumes:

```text
martie-dev-channer  -> /data/... on martie-dev-channer-data
martie-prod-threadnotifier  -> /data/... on martie-prod-threadnotifier-data
```

Docker health checks call `martie check-health`, which requests `/healthz` on
the process-level HTTP server. Keep `runtime.http_addr = ":9090"` in Docker
configs, or set `HEALTHCHECK_ADDR` and `runtime.http_addr` to matching
addresses.

Docker logging defaults to the rotating `local` driver, capped at five 10 MB files per container. This is safe without host setup, but removing a container removes its history. On a systemd server, use journald to retain logs across deployments:

```bash
make docker-deploy MARTIE_ENV=prod DOCKER_LOG_DRIVER=journald
make docker-logs MARTIE_ENV=prod DOCKER_LOG_DRIVER=journald
```

Ensure the host journal is persistent and bounded with `/etc/systemd/journald.conf.d/martie.conf`:

```ini
[Journal]
Storage=persistent
SystemMaxUse=500M
MaxRetentionSec=30day
```

Apply the host configuration with `sudo systemctl restart systemd-journald`. In journald mode, `make docker-logs` runs `journalctl`; the current user therefore needs journal access. If it is denied, use `sudo journalctl -t martie-prod -f` or grant the user the host's journal-reader group. Historical logs can be queried with `journalctl -t martie-prod --since yesterday`. Hosts without journald should keep the default `local` driver.

To scrape Martie from Prometheus in another container, set `runtime.http_addr = ":9090"` and attach both containers to the same user-defined Docker network:

```bash
docker network create monitoring # once, unless the network already exists
make docker-deploy MARTIE_ENV=prod DOCKER_NETWORK=monitoring
```

Prometheus can then scrape `martie-prod:9090` without publishing the port on the host. `DOCKER_NETWORK` must name an existing network. For a host-based or external Prometheus, publish the port explicitly with `DOCKER_RUN_EXTRA`; health is available at `/healthz`, readiness at `/readyz`, and metrics at `/metrics`. Readiness returns 503 until the selected app has finished initialization.

## Metrics

`GET /metrics` exposes Prometheus text metrics when `runtime.http_addr` is
set.

Use Prometheus's scrape-generated `up` series for process reachability.

Recurring operations:

- `martie_operation_duration_seconds{operation,result}` measures recurring
  Telegram and stream polling. Its histogram `_count` series is the operation
  counter.
- `martie_operation_last_success{operation}` is `1` when the last operation
  succeeded and `0` when it failed.
- `martie_operation_last_completed_timestamp_seconds{operation}` detects
  stalled operations without leaving stale per-result timestamp series.

Gateway ingestion:

- `martie_gateway_webhook_requests_total{result}` counts all webhook requests.
  Results are `success`, `method_not_allowed`, `bad_request`,
  `payload_too_large`, `unauthorized`, `invalid_event`, or `consumer_error`.
- `martie_gateway_event_dispatches_total{consumer,kind,result}` counts decoded
  gateway events dispatched to enabled consumers.

Notifications:

- `martie_notification_delivery_attempts_total{source,result}` counts every
  Telegram notification send attempt, including stream and thread-notifier
  failures.

Assistants:

- `martie_assistant_admissions_total{surface,result}` counts admission
  decisions before model calls. `surface` is `chatter` or
  `channer`. Duplicate deliveries for already-recorded channer
  work use `result="duplicate"`.
- `martie_assistant_reply_deliveries_total{surface,result}` counts public or
  Telegram assistant reply delivery attempts.
- `martie_assistant_context_uses_total{surface,type}` counts request-scoped
  context actually included in model prompts, such as `history`, `reply`, or
  `ptchan`.
- `martie_assistant_active_conversations{surface}` tracks in-memory
  conversation histories.
- `martie_channer_requests_total{outcome}` counts terminal outcomes for
  admitted public requests. Outcomes are `posted`,
  `local_global_rate_limited`, `local_thread_rate_limited`,
  `gateway_rate_limited`, `completion_error`,
  `completion_rejected`, `posting_rejected`, `posting_unknown`, or
  `not_configured`. Exact gateway failure codes remain in the SQLite event
  ledger rather than becoming metric labels.

Model calls:

- `martie_model_completion_duration_seconds{surface,provider,model,outcome}`
  measures only the completion API call. Its histogram `_count` series counts
  calls. Outcomes are model finish reasons, `error`, or `unknown`.
- `martie_model_tokens_total{surface,provider,model,type}` counts model token
  usage. Token types currently include `input_cache_hit`, `input_cache_miss`,
  and `output`.

Metrics must not expose Telegram message content, DeepSeek prompts or
responses, gateway signatures, ptchan payload bodies, or per-user labels.

## Telegram setup notes

The notification chat receives gateway and stream announcements. The discussion chat is where `chatter` listens for mentions and replies.

To receive ordinary group mentions, make the bot a group administrator or disable Group Privacy in BotFather. If you do not know the discussion chat ID, run Martie, mention it in the group, and inspect the debug log for the observed chat ID.

Access to `chatter` is fail-closed by default. Configure `telegram.allowed_user_ids`, or set `telegram.allow_all_users = true` intentionally.

When `chatter` is enabled, addressed message text and recent conversation context are sent to the configured DeepSeek API. Telegram identities are replaced with temporary aliases, but message content is not anonymized.

Use `chatter.system_prompt` for Martie's Telegram personality, tone, boundaries, and general response style. Telegram discussion behavior, participant aliases, reply context, memory, and ptchan transcript rules are generated as bounded context packets by Martie.

`chatter` enriches requests that contain ptchan thread links by reading signed
sanitized thread data from ptchan-gateway using the selected app's integration.
It renders a bounded context packet for the current completion only; fetched
thread data and rendered context are not persisted in conversation history.
`chatter.memory.ttl` controls how long exchanges remain in process memory, while
`history_exchanges` caps the user/assistant pairs retained per Telegram topic.

Shared ptchan context uses the gateway's `origin` field to label posts as
`SELF` when the origin matches this process's selected `ptchan.integration_name`
and as `INTEGRATION <name>` otherwise. Configure each posting integration's unique
`posting.public_tripcode` in ptchan-gateway so webhook and thread-read payloads
can identify integration output. The secure tripcode remains in the gateway's
`PTCHAN_INTEGRATION_<NAME>_TRIPCODE` environment variable; Martie neither needs
nor receives it. Remove the obsolete `ptchan.self_tripcodes` key from existing
Martie configs; strict decoding rejects it.

Simultaneous identities must use distinct gateway integrations. For example,
Marta dev and Martie prod should have separate integration names, public
tripcodes, integration secrets, and tripcode secrets. Their TOML `name` values
control assistant identity in prompts; their selected `ptchan.integration_name`
values control gateway authentication and `SELF` origin matching.

`channer` has its own `[channer]` config with configurable `mentions` and a
separate `system_prompt`. It always reads the triggering thread through the
gateway. `prune_after` controls retention of its one-shot event ledger; `0s`
disables pruning. Gateway posting is thread-level, so ptchan replies target a
post by including a `>>post_id` reference in the generated message.

## ptchan Assistant Notes

`channer` is intentionally separate from `chatter` because
public ptchan replies need different defaults and guardrails.

Mentions are configured with `channer.mentions` and should be matched
case-insensitively. The default is `@martie`.

The gateway posting contract is thread-level:

```http
POST /integration/v1/threads/:board/:thread_id/replies
```

with a JSON body like:

```json
{ "message": ">>405\nreply text", "sage": false }
```

There is no separate reply-to-post field. A channer reply should target
the triggering post by including a `>>post_id` reference in the generated
message and posting to the thread. Channer prepends that reference itself; the
model generates only the answer.

`channer` does not spawn from gateway events whose post origin is
`integration`, regardless of which integration produced the post. Integration
posts still count as normal posts for gateway thread tracking and notification
thresholds; this rule only controls channer invocation. Martie trusts the
gateway's deterministic origin attribution instead of matching tripcodes or
remembering posted coordinates locally. It records admitted assistant work in
SQLite before model calls or posting; ignored events stay transient, and
duplicate deliveries for admitted work are acknowledged without repeating
assistant side effects. Fetched context remains request-scoped and transient.
Gateway payloads are already sanitized public data; Martie additionally removes
unsafe control characters before placing post text inside bounded prompt fences.

Public ptchan replies use global and per-thread token-bucket limits configured
in `[channer.rate_limit]`. The per-thread budget prevents one busy or adversarial
thread from consuming the process-wide budget. Martie does not attempt per-user
rate limits because ptchan posts are anonymous. These in-memory limits reset
when the process restarts, and inactive thread buckets are discarded after an
hour.

`channer` is one-shot: each admitted gateway event gets at most one
completion attempt and one public posting attempt. Completion failures, local
rate-limit denials, and structured gateway posting failures are recorded as final
failures and acknowledged; repeated webhook deliveries are treated as duplicates.
If ptchan-gateway reports `reply_state_unknown`, or if the post attempt fails in
a way Martie cannot classify, Martie records the event as unknown and still does
not retry automatically because ptchan may already have accepted the reply. This
deliberately favors avoiding duplicate public posts over eventual delivery.

## Development

```bash
make check   # format, vet, and test
make build
```

See `make help` for the complete command list.

## License

GNU General Public License, version 3 or later. See `LICENSE`.
