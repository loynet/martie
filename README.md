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

The Go code should stay plain and strict while it grows. Shared pieces should be
extracted when there are real consumers, especially model completion, prompt
tracing, assistant context rendering, signed gateway clients, idempotency
ledgers, and rate limiting. Avoid turning the two assistants into a generic framework too
early: Telegram and ptchan have different identity, admission, privacy, and
reply semantics.

Persistence follows the same bias. Martie uses one SQLite database by default,
but `storage.chatter`, `storage.channer`,
`storage.threadnotifier`, and `storage.streamnotifier` can point at separate files when the
apps have separate lifecycles, retention policies, or write profiles.

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
make docker-traces MARTIE_ENV=prod
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
mounted config to store SQLite at `/data/martie.db`; assistant traces are written
under `/data/traces` on the named Docker volume.
`docker-deploy` replaces the container but keeps the volume.

Different apps and environments can run on the same host at the same time. They
see the same SQLite and trace paths inside their containers, but those paths are
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

Prometheus can then scrape `martie-prod:9090` without publishing the port on the host. `DOCKER_NETWORK` must name an existing network. For a host-based or external Prometheus, publish the port explicitly with `DOCKER_RUN_EXTRA`; health is available at `/healthz`, readiness at `/readyz`, and metrics at `/metrics`.

## Metrics

`GET /metrics` exposes Prometheus text metrics when `runtime.http_addr` is
set.

The metrics were reshaped around Martie's workers and assistant surfaces. If
you had dashboards for earlier `martie_component_*`, `martie_workflow_*`,
`martie_ai_*`, `martie_notifications_sent_total`, or
`martie_assistant_*updates/responses` names, update them to the families below.

Process:

- `martie_up`: constant `1` while the process is serving metrics.

Workers:

- `martie_worker_runs_total{worker,result}` counts completed worker
  runs.
- `martie_worker_run_duration_seconds{worker}` observes worker run
  duration.
- `martie_worker_last_run_success{worker}` is `1` when the last observed
  run succeeded and `0` when it failed.
- `martie_worker_last_run_timestamp_seconds{worker,result}` records when
  the last run finished.

Gateway ingestion:

- `martie_gateway_webhooks_total{result}` counts signed webhook requests,
  including rejection reasons such as `unauthorized`, `bad_event`, and
  `consumer_error`.
- `martie_gateway_events_total{consumer,kind,result}` counts decoded gateway
  events dispatched to enabled consumers such as `threadnotifier` or
  `channer`.

Notifications:

- `martie_notifications_total{source,result}` counts delivery attempts for
  Telegram-facing notifications. `source` is the app that produced the
  notification, such as `threadnotifier` or `streamnotifier`.

Assistants:

- `martie_assistant_admissions_total{surface,result}` counts admission
  decisions before model calls. `surface` is `chatter` or
  `channer`. Duplicate deliveries for already-recorded channer
  work use `result="duplicate"`.
- `martie_assistant_replies_total{surface,result}` counts assistant reply
  delivery attempts.
- `martie_assistant_context_total{surface,type}` counts request-scoped context
  used in model prompts, such as `history`, `reply`, or `ptchan`.
- `martie_assistant_active_conversations{surface}` tracks in-memory
  conversation histories.

Model calls:

- `martie_model_requests_total{surface,provider,model,result,finish_reason}`
  counts model requests. Errors use an empty `finish_reason`.
- `martie_model_request_duration_seconds{surface,provider,model}` observes
  model request latency.
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

`chatter` can optionally enrich requests that contain ptchan thread links. When `[chatter.ptchan_context]` is present, Martie reads signed sanitized thread data from ptchan-gateway using the selected app's ptchan integration, renders a bounded assistant context packet with ptchan format notes, a conversation map, fenced post bodies, and response rules, then sends that only for the current completion. The fetched thread data and rendered context are not persisted in conversation history.

If `ptchan.self_tripcodes` is configured, shared ptchan context labels matching
posts as `SELF`. The tripcodes are public identity markers, not secrets. This
helps Telegram and `channer` prompts distinguish the assistant's own prior
public posts from new user requests.

`channer` has its own `[channer]` config with configurable `mentions`, a separate `system_prompt`, and its own ptchan context and trace sections. Gateway posting is thread-level, so ptchan replies target a post by including a `>>post_id` reference in the generated message.

For local prompt inspection, include `[chatter.trace]` or `[channer.trace]` in TOML. Martie then writes one private, human-readable trace for every assistant interaction sent to the model and logs its path. Each trace separates stored conversation state from the exact model request and result. Traces contain private message and prompt content and are disabled by default. `*.trace.max_files` controls retention.

Local runs write traces to the configured trace directory. Docker writes to `/data/traces` when the mounted config uses `dir = "data/traces"`; that directory is not directly visible in the host checkout. Run `make docker-traces MARTIE_ENV=dev` (or `MARTIE_ENV=prod`) to copy the current traces into the host's `data/traces`.

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
message and posting to the thread.

`channer` does not spawn from gateway events whose post origin is
`integration`, regardless of which integration produced the post. Integration
posts still count as normal posts for gateway thread tracking and notification
thresholds; this rule only controls channer invocation. Configure
`ptchan.self_tripcodes` with the assistant's public ptchan tripcodes so fetched
ptchan context can clearly label the assistant's own public posts. Without a
configured tripcode match, Martie treats the post as ordinary context. It records
admitted assistant work in SQLite before model calls or posting; ignored events
stay transient, and duplicate deliveries for admitted work are acknowledged
without repeating assistant side effects. Fetched context should remain transient
unless tracing is explicitly enabled.

Public ptchan replies use a global token-bucket limit configured in
`[channer.rate_limit]`. Because ptchan posts are anonymous, Martie does
not attempt per-user rate limits on this surface.

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
