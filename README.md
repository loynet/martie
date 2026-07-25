# martie

Martie is a small ptchan community automation service. It can run several independent components:

- `gateway` consumes signed ptchan-gateway events and announces eligible threads to Telegram.
- `streams` watches configured stream URLs and announces when they go live.
- `telegram_assistant` answers messages addressed to Martie in a Telegram discussion group using DeepSeek.
- `ptchan_assistant` is the planned ptchan-native assistant surface for replying when Martie is mentioned on ptchan.

Martie uses long polling for Telegram where needed, stores its small amount of durable state in SQLite, receives private webhooks from ptchan-gateway, and can expose Prometheus metrics. Internally, signed ptchan events are received once and dispatched to whichever ptchan-event consumers are enabled.

## Direction

Martie is moving from a Telegram-centered bot into a small, multi-surface
automation service for the ptchan community. Telegram remains important, but it
is one delivery and conversation surface rather than the whole product shape.

The next major feature is a ptchan-native assistant. The intended flow is:

1. ptchan-gateway observes a new ptchan post and sends Martie a signed webhook.
2. Martie admits the post only if it mentions a configured ptchan assistant name.
3. Martie reads sanitized thread data from ptchan-gateway.
4. Martie asks the configured model using a ptchan-specific system prompt and
   bounded context packet.
5. Martie replies through ptchan-gateway's signed posting API.

ptchan-gateway is the ptchan boundary. Martie should not fetch or post to ptchan
directly; new ptchan behavior should go through signed gateway APIs. The gateway
already models the useful pieces for this direction: explicit integration
capabilities, board-scoped access, reading and posting rate limits, origin
tracking for produced posts, and public sanitized contracts.

The Go code should stay plain and strict while it grows. Shared pieces should be
extracted when there are real consumers, especially model completion, prompt
tracing, assistant context rendering, signed gateway clients, retry queues, and
rate limiting. Avoid turning the two assistants into a generic framework too
early: Telegram and ptchan have different identity, admission, privacy, and
reply semantics.

Persistence follows the same bias. Martie uses one SQLite database by default
because current state is small and operationally co-owned. Separate databases
are on the table when a component earns a separate lifecycle, retention policy,
privacy boundary, or write profile, but ordinary workflows should not need to
join data across multiple files without a concrete payoff.

## Run locally

Requirements: Go 1.25 or newer, a Telegram bot token when running Telegram-backed components, and a DeepSeek API key when running assistant components.

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
- `runtime.components` selects `gateway`, `streams`, `telegram_assistant`, `ptchan_assistant`, or any supported combination.

The example TOML documents every setting. Copy it, then remove optional sections or `runtime.components` entries you do not want. Unknown keys and invalid values fail at startup. `MARTIE_ENV=prod` selects `.env.prod` and `config/prod.toml`. `BOT_ENV` is still accepted by the Makefile as an older alias.

Validate an environment locally before running it:

```bash
make check-config MARTIE_ENV=prod
```

On first startup, the gateway component records its bootstrap time and suppresses older webhook events. New events observed after that point are processed normally.

The gateway webhook listener starts when a selected component consumes ptchan
events, such as `gateway` or `ptchan_assistant`. The `gateway` component itself
tracks threads and sends Telegram notifications; the shared listener owns
signature verification and event dispatch.

Component dependencies are deliberately checked only for selected components.
For example, `ptchan_assistant` is designed to run without Telegram, while
`telegram_assistant`, `gateway`, and `streams` still need Telegram settings for
their Telegram-facing behavior.

## Deploy with Docker

Create `.env.prod` and `config/prod.toml`, then deploy:

```bash
make docker-deploy MARTIE_ENV=prod
```

`docker-deploy` builds the image, validates the selected environment with
`martie check-config` inside that image, then replaces the container only after
the check passes.

Useful operational commands:

```bash
make docker-logs MARTIE_ENV=prod
make docker-traces MARTIE_ENV=prod
make docker-clean
```

The container runs as a non-root user with a read-only filesystem. The selected TOML file is mounted read-only, secrets are passed through the environment, and SQLite is stored in the persistent `martie-prod-data` volume.

Docker images are tagged with the current commit by default, for example
`martie:abc1234`. Override `IMAGE` when pushing to a registry or using a
specific tag:

```bash
make docker-deploy MARTIE_ENV=prod IMAGE=registry.example/martie:abc1234
```

`MARTIE_ENV` selects the environment-specific inputs and resource names:

```text
MARTIE_ENV=dev   -> .env.dev,  config/dev.toml,  martie-dev,  martie-dev-data
MARTIE_ENV=prod  -> .env.prod, config/prod.toml, martie-prod, martie-prod-data
```

Inside the container, the selected config is mounted read-only at
`/etc/martie/config.toml`. Set `storage.sqlite_path = "data/martie.db"` in the
mounted config to store SQLite at `/data/martie.db`; assistant traces are written
under `/data/traces` on the named Docker volume.
`docker-deploy` replaces the container but keeps the volume.

Dev and prod can run on the same host at the same time. They see the same
SQLite and trace paths inside their containers, but those paths are backed by
different named volumes:

```text
martie-dev   -> /data/martie.db and /data/traces on martie-dev-data
martie-prod  -> /data/martie.db and /data/traces on martie-prod-data
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

The metrics were reshaped around Martie's component and assistant surfaces. If
you had dashboards for the earlier `martie_workflow_*`, `martie_ai_*`,
`martie_notifications_sent_total`, or `martie_assistant_*updates/responses`
names, update them to the families below.

Process:

- `martie_up`: constant `1` while the process is serving metrics.

Components:

- `martie_component_runs_total{component,result}` counts completed component
  runs.
- `martie_component_run_duration_seconds{component}` observes component run
  duration.
- `martie_component_last_run_success{component}` is `1` when the last observed
  run succeeded and `0` when it failed.
- `martie_component_last_run_timestamp_seconds{component,result}` records when
  the last run finished.

Gateway ingestion:

- `martie_gateway_webhooks_total{result}` counts signed webhook requests,
  including rejection reasons such as `unauthorized`, `bad_event`, and
  `consumer_error`.
- `martie_gateway_events_total{consumer,kind,result}` counts decoded gateway
  events dispatched to enabled consumers such as `gateway` or
  `ptchan_assistant`.

Notifications:

- `martie_notifications_total{source,result}` counts delivery attempts for
  Telegram-facing notifications. `source` is the component that produced the
  notification, such as `gateway` or `streams`.

Assistants:

- `martie_assistant_admissions_total{surface,result}` counts admission
  decisions before model calls. `surface` is `telegram_assistant` or
  `ptchan_assistant`.
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

The notification chat receives gateway and stream announcements. The discussion chat is where the Telegram assistant listens for mentions and replies.

To receive ordinary group mentions, make the bot a group administrator or disable Group Privacy in BotFather. If you do not know the discussion chat ID, run Martie, mention it in the group, and inspect the debug log for the observed chat ID.

Access to the Telegram assistant is fail-closed by default. Configure `telegram.allowed_user_ids`, or set `telegram.allow_all_users = true` intentionally.

When the Telegram assistant is enabled, addressed message text and recent conversation context are sent to the configured DeepSeek API. Telegram identities are replaced with temporary aliases, but message content is not anonymized.

Use `telegram_assistant.system_prompt` for Martie's Telegram personality, tone, boundaries, and general response style. Telegram discussion behavior, participant aliases, reply context, memory, and ptchan transcript rules are generated as bounded context packets by Martie.

The Telegram assistant can optionally enrich requests that contain ptchan thread links. When `[telegram_assistant.ptchan_context]` is present, Martie reads signed sanitized thread data from ptchan-gateway using the top-level `[ptchan]` settings, renders a bounded assistant context packet with ptchan format notes, a conversation map, fenced post bodies, and response rules, then sends that only for the current completion. The fetched thread data and rendered context are not persisted in conversation history.

The planned ptchan assistant has its own `[ptchan_assistant]` config with configurable `mentions`, a separate `system_prompt`, and its own ptchan context and trace sections. Gateway posting is thread-level, so ptchan replies should target a post by including a `>>post_id` reference in the generated message.

For local prompt inspection, include `[telegram_assistant.trace]` or `[ptchan_assistant.trace]` in TOML. Martie then writes one private, human-readable trace for every assistant interaction sent to the model and logs its path. Each trace separates stored conversation state from the exact model request and result. Traces contain private message and prompt content and are disabled by default. `*.trace.max_files` controls retention.

Local runs write traces to the configured trace directory. Docker writes to `/data/traces` when the mounted config uses `dir = "data/traces"`; that directory is not directly visible in the host checkout. Run `make docker-traces MARTIE_ENV=dev` (or `MARTIE_ENV=prod`) to copy the current traces into the host's `data/traces`.

## ptchan Assistant Notes

`ptchan_assistant` is currently configuration and runtime scaffolding for the
upcoming ptchan-native assistant. Its config is intentionally separate from
`telegram_assistant` because public ptchan replies need different defaults and
guardrails.

Mentions are configured with `ptchan_assistant.mentions` and should be matched
case-insensitively. The default is `@martie`.

The gateway posting contract is thread-level:

```http
POST /integration/v1/threads/:board/:thread_id/replies
```

with a JSON body like:

```json
{ "message": ">>405\nreply text", "sage": false }
```

There is no separate reply-to-post field. A ptchan assistant reply should target
the triggering post by including a `>>post_id` reference in the generated
message and posting to the thread.

The ptchan assistant should ignore posts produced by Martie's own gateway
integration origin so it does not answer itself. It should dedupe gateway events
before model calls and posting, and it should treat fetched context as transient
unless tracing is explicitly enabled.

## Development

```bash
make check   # format, vet, and test
make build
```

See `make help` for the complete command list.

## License

GNU General Public License, version 3 or later. See `LICENSE`.
