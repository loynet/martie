# Martie

Martie is a public ptchan assistant. When someone uses one of its configured
mentions in a new post, Martie receives the event from ptchan-gateway, reads a
safe copy of the thread, asks the configured model for a response, and posts a
reply.

Martie is intentionally a single-purpose service. It does not scrape ptchan or
talk to it directly: ptchan-gateway is its signed boundary for events, thread
context, and replies.

## Before you start

You need:

- Go for local development, or Docker for deployment.
- A ptchan-gateway integration with permission to receive events, read threads,
  and post replies.
- A DeepSeek API key.

## Run locally

Create a local configuration and secrets file:

```sh
cp config/example.toml config/dev.toml
cp .env.example .env.dev
```

Edit `config/dev.toml` to choose Martie's name, mentions, gateway addresses,
and SQLite path. Edit `.env.dev` with the secrets. Then validate the setup and
start Martie:

```sh
make check-config
make run
```

`MARTIE_ENV=prod` selects `config/prod.toml` and `.env.prod`. The complete
setting reference is [config/example.toml](config/example.toml).

Configure the gateway integration to send webhooks to:

```text
https://your-martie-host/internal/ptchan/events
```

The integration secret variable is derived from `ptchan.integration_name`:
`martie` becomes `PTCHAN_INTEGRATION_MARTIE_SECRET`; `-` and `.` become `_`.

## What happens to an event

1. Martie verifies the signed webhook.
2. It ignores unsupported events, integration-origin posts, unaddressed posts,
   blank posts, and posts above the configured size limit.
3. It stores admitted event IDs in SQLite before model or posting work.
4. It reads bounded, sanitized thread context through ptchan-gateway.
5. It asks DeepSeek for a response and posts the result through ptchan-gateway.

The SQLite ledger makes repeated deliveries of an admitted event safe. Model,
posting, and unknown posting outcomes are final: Martie deliberately does not
retry them automatically, because retrying could create a duplicate public
reply.

## Docker

For production, create `.env.prod` and `config/prod.toml`, then deploy:

```sh
make docker-deploy MARTIE_ENV=prod
```

This validates configuration before replacing the container. The container runs
as a non-root user with a read-only filesystem; its SQLite database is stored in
the persistent `martie-prod-data` volume. Useful commands:

```sh
make docker-logs MARTIE_ENV=prod
make docker-clean
```

Set `DOCKER_NETWORK=monitoring` to join an existing Docker network for
Prometheus scraping. Docker uses bounded local logs by default; set
`DOCKER_LOG_DRIVER=journald` on hosts with a persistent, managed system journal.
Martie writes structured JSON logs to stdout.

## Health and metrics

Set `runtime.http_addr` to expose:

- `/healthz` — the process is running.
- `/readyz` — Martie has initialized its state and gateway listener.
- `/metrics` — Prometheus metrics.

The operational metrics are:

- `martie_gateway_webhook_requests_total{result}` — webhook results.
- `martie_gateway_event_deliveries_total{kind,result}` — decoded event delivery.
- `martie_channer_admissions_total{result}` — admission decisions.
- `martie_channer_reply_deliveries_total{result}` — posting attempts.
- `martie_channer_context_uses_total{type}` — context sources used.
- `martie_channer_outcomes_total{outcome}` — terminal outcome for admitted work.
- `martie_model_completion_duration_seconds{provider,model,outcome}` and
  `martie_model_tokens_total{provider,model,type}` — model use.

Metrics never contain post IDs, user identities, prompts, replies, or webhook
signatures.
