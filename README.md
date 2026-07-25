# martie

Martie is a small Telegram bot for the ptchan community. It can run three independent components:

- `gateway` receives signed ptchan-gateway webhooks and announces eligible threads.
- `streams` watches configured stream URLs and announces when they go live.
- `assistant` answers messages addressed to the bot in a Telegram discussion group using DeepSeek.

Martie uses long polling for Telegram, stores its small amount of durable state in SQLite, receives private webhooks from ptchan-gateway, and can expose Prometheus metrics.

## Run locally

Requirements: Go 1.25 or newer, a Telegram bot token, and a DeepSeek API key when running the assistant.

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
- `runtime.components` selects `gateway`, `streams`, `assistant`, or any combination.

The example TOML documents every setting. Copy it, then remove optional sections or `runtime.components` entries you do not want. Unknown keys and invalid values fail at startup. `BOT_ENV=prod` selects `.env.prod` and `config/prod.toml`.

Validate an environment locally before running it:

```bash
make check-config BOT_ENV=prod
```

On first startup, the gateway component records its bootstrap time and suppresses older webhook events. New events observed after that point are processed normally.

## Deploy with Docker

Create `.env.prod` and `config/prod.toml`, then deploy:

```bash
make docker-deploy BOT_ENV=prod
```

`docker-deploy` builds the image, validates the selected environment with
`martie check-config` inside that image, then replaces the container only after
the check passes.

Useful operational commands:

```bash
make docker-logs BOT_ENV=prod
make docker-traces BOT_ENV=prod
make docker-clean
```

The container runs as a non-root user with a read-only filesystem. The selected TOML file is mounted read-only, secrets are passed through the environment, and SQLite is stored in the persistent `martie-prod-data` volume.

Docker images are tagged with the current commit by default, for example
`martie:abc1234`. Override `IMAGE` when pushing to a registry or using a
specific tag:

```bash
make docker-deploy BOT_ENV=prod IMAGE=registry.example/martie:abc1234
```

`BOT_ENV` selects the environment-specific inputs and resource names:

```text
BOT_ENV=dev   -> .env.dev,  config/dev.toml,  martie-dev,  martie-dev-data
BOT_ENV=prod  -> .env.prod, config/prod.toml, martie-prod, martie-prod-data
```

Inside the container, the selected config is mounted read-only at
`/etc/martie/config.toml`. Set `storage.sqlite_path = "data/bot.db"` in the
mounted config to store SQLite at `/data/bot.db`; assistant traces are written
under `/data/traces` on the named Docker volume.
`docker-deploy` replaces the container but keeps the volume.

Dev and prod can run on the same host at the same time. They see the same
SQLite and trace paths inside their containers, but those paths are backed by
different named volumes:

```text
martie-dev   -> /data/bot.db and /data/traces on martie-dev-data
martie-prod  -> /data/bot.db and /data/traces on martie-prod-data
```

Docker health checks call `martie check-health`, which requests `/healthz` on
the process-level HTTP server. Keep `runtime.http_addr = ":9090"` in Docker
configs, or set `HEALTHCHECK_ADDR` and `runtime.http_addr` to matching
addresses.

Docker logging defaults to the rotating `local` driver, capped at five 10 MB files per container. This is safe without host setup, but removing a container removes its history. On a systemd server, use journald to retain logs across deployments:

```bash
make docker-deploy BOT_ENV=prod DOCKER_LOG_DRIVER=journald
make docker-logs BOT_ENV=prod DOCKER_LOG_DRIVER=journald
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
make docker-deploy BOT_ENV=prod DOCKER_NETWORK=monitoring
```

Prometheus can then scrape `martie-prod:9090` without publishing the port on the host. `DOCKER_NETWORK` must name an existing network. For a host-based or external Prometheus, publish the port explicitly with `DOCKER_RUN_EXTRA`; health is available at `/healthz`, readiness at `/readyz`, and metrics at `/metrics`.

## Metrics

`GET /metrics` exposes Prometheus text metrics when `runtime.http_addr` is
set.

- Process health: `martie_up`.
- Component runs: `martie_workflow_runs_total`,
  `martie_workflow_duration_seconds`, `martie_workflow_last_success`, and
  `martie_workflow_last_successful_timestamp_seconds`.
- Notifications: `martie_notifications_sent_total`, labeled by source.
- Assistant admission and delivery: `martie_assistant_updates_total`,
  `martie_assistant_responses_total`,
  `martie_assistant_context_requests_total`, and
  `martie_assistant_active_conversations`.
- AI usage and latency: `martie_ai_requests_total`,
  `martie_ai_request_duration_seconds`, and `martie_ai_tokens_total`.

Metrics must not expose Telegram message content, DeepSeek prompts or
responses, gateway signatures, ptchan payload bodies, or per-user labels.

## Telegram setup notes

The notification chat receives gateway and stream announcements. The discussion chat is where the assistant listens for mentions and replies.

To receive ordinary group mentions, make the bot a group administrator or disable Group Privacy in BotFather. If you do not know the discussion chat ID, run Martie, mention it in the group, and inspect the debug log for the observed chat ID.

Access to the assistant is fail-closed by default. Configure `telegram.allowed_user_ids`, or set `telegram.allow_all_users = true` intentionally.

When the assistant is enabled, addressed message text and recent conversation context are sent to the configured DeepSeek API. Telegram identities are replaced with temporary aliases, but message content is not anonymized.

Use `assistant.system_prompt` for Martie's personality, tone, boundaries, and general response style. Telegram discussion behavior, participant aliases, reply context, memory, and ptchan transcript rules are generated as bounded context packets by Martie.

The assistant can optionally enrich requests that contain ptchan thread links. When `[assistant.ptchan_context]` is present, Martie asks ptchan-gateway for signed sanitized thread context using the top-level `[ptchan]` settings, renders a bounded packet with ptchan format notes, a conversation map, fenced post bodies, and response rules, then sends that only for the current completion. The fetched context is not persisted in conversation history.

For local prompt inspection, include `[assistant.trace]` in TOML. Martie then writes one private, human-readable trace for every assistant interaction sent to the model and logs its path. Each trace separates stored conversation state from the exact model request and result. Traces contain private message and prompt content and are disabled by default. `assistant.trace.max_files` controls retention.

Local runs write traces to `assistant.trace.dir`. Docker writes to `/data/traces` when the mounted config uses `dir = "data/traces"`; that directory is not directly visible in the host checkout. Run `make docker-traces BOT_ENV=dev` (or `BOT_ENV=prod`) to copy the current traces into the host's `data/traces`.

## Development

```bash
make check   # format, vet, and test
make build
```

See `make help` for the complete command list.

## License

GNU General Public License, version 3 or later. See `LICENSE`.
