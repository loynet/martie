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
cp config.example.toml config/dev.toml
```

Edit both files, then run:

```bash
make run
```

Configuration is split deliberately:

- `.env.dev` contains secrets.
- `config/dev.toml` contains application settings.
- `runtime.components` selects `gateway`, `streams`, `assistant`, or any combination.

The example TOML documents every setting. Unknown keys and invalid values fail at startup. `BOT_ENV=prod` selects `.env.prod`, `config/prod.toml`, and `data/prod.db`.

On first startup, the gateway component records its bootstrap time and suppresses older webhook events. New events observed after that point are processed normally.

## Deploy with Docker

Create `.env.prod` and `config/prod.toml`, then deploy:

```bash
make docker-deploy BOT_ENV=prod
```

Useful operational commands:

```bash
make docker-logs BOT_ENV=prod
make docker-traces BOT_ENV=prod
make docker-clean
```

The container runs as a non-root user with a read-only filesystem. The selected TOML file is mounted read-only, secrets are passed through the environment, and SQLite is stored in the persistent `martie-prod-data` volume.

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

To scrape Martie from Prometheus in another container, set `runtime.metrics_addr = ":9090"` and attach both containers to the same user-defined Docker network:

```bash
docker network create monitoring # once, unless the network already exists
make docker-deploy BOT_ENV=prod DOCKER_NETWORK=monitoring
```

Prometheus can then scrape `martie-prod:9090` without publishing the port on the host. `DOCKER_NETWORK` must name an existing network. For a host-based or external Prometheus, publish the port explicitly with `DOCKER_RUN_EXTRA`; metrics are available at `/metrics`.

## Telegram setup notes

The notification chat receives gateway and stream announcements. The discussion chat is where the assistant listens for mentions and replies.

To receive ordinary group mentions, make the bot a group administrator or disable Group Privacy in BotFather. If you do not know the discussion chat ID, run Martie, mention it in the group, and inspect the debug log for the observed chat ID.

Access to the assistant is fail-closed by default. Configure `telegram.allowed_user_ids`, or set `telegram.allow_all_users = true` intentionally.

When the assistant is enabled, addressed message text and recent conversation context are sent to the configured DeepSeek API. Telegram identities are replaced with temporary aliases, but message content is not anonymized.

Use `assistant.system_prompt` for Martie's personality, tone, boundaries, and general response style. Telegram discussion behavior, participant aliases, reply context, memory, and ptchan transcript rules are generated as bounded context packets by Martie.

The assistant can optionally enrich requests that contain ptchan thread links. When `assistant.ptchan_context.enabled` is true, Martie asks ptchan-gateway for signed sanitized thread context, renders a bounded packet with ptchan format notes, a conversation map, fenced post bodies, and response rules, then sends that only for the current completion. The fetched context is not persisted in conversation history.

For local prompt inspection, set `assistant.trace.enabled = true` in TOML. Martie then writes one private, human-readable trace for every assistant interaction sent to the model and logs its path. Each trace separates stored conversation state from the exact model request and result. Traces contain private message and prompt content and are disabled by default. `assistant.trace.max_files` controls retention.

Local runs write to `data/traces` by default. Docker writes to `/data/traces` inside its named volume; that directory is not directly visible in the host checkout. Run `make docker-traces BOT_ENV=dev` (or `BOT_ENV=prod`) to copy the current traces into the host's `data/traces`. `MARTIE_ASSISTANT_TRACE_DIR` can override the path for non-Docker deployments.

## Development

```bash
make check   # format, vet, and test
make build
```

See `make help` for the complete command list.

## License

GNU General Public License, version 3 or later. See `LICENSE`.
