# AGENTS.md

Martie is one public ptchan responder. Keep it small, direct, and idiomatic.
Follow [Effective Go](https://go.dev/doc/effective_go): visible data flow,
concrete types, early returns, and local error handling beat frameworks,
factories, builders, and speculative interfaces.

## Boundaries

- `cmd/martie` owns command parsing and exit codes.
- `internal/app` owns strict config loading, process wiring, health/metrics HTTP
  endpoints, and signed gateway webhook reception. It does not own reply policy.
- `internal/channer` owns mention admission, rate limiting, model completion,
  public posting, and idempotency decisions.
- `internal/channer/state` owns the Channer SQLite schema and event ledger.
  `internal/storage` is SQLite setup and low-level helpers only.
- `github.com/loynet/ptchan-gateway/clients/go` owns the gateway contract,
  signing, thread reads, and posts. Do not recreate gateway types or signing.
- `github.com/loynet/ptchan-ai` owns DeepSeek transport and request-scoped
  thread-context rendering. Do not copy either package locally.

Do not fetch ptchan directly. Keep protocol conversion and public-reply policy
in Channer, not in `app` or `storage`.

## Public side effects

The event ledger is the boundary around public replies. Store admitted work
before completion or posting; treat repeated admitted events as duplicates.
Ignored events remain transient. Completion, posting, and unknown posting
outcomes are final and acknowledged. Do not add automatic retries without an
explicit duplicate-post product decision.

Use the official gateway client's documented verification rules. Validate only
documented consumer invariants; do not reject valid future additive fields.

## Configuration and operations

TOML holds non-secret deployment policy. Secrets are `DEEPSEEK_API_KEY` and
`PTCHAN_INTEGRATION_<INTEGRATION_NAME>_SECRET`. Keep TOML strict and validate
semantic values at startup. Martie has one runtime command: `run`.

Metrics are public contracts. Use low-cardinality labels only; never label with
identities, prompts, thread/post IDs, request bodies, or signatures. Keep the
README metrics list accurate. Log JSON to stdout; do not add application log
files.

## Change discipline

Prefer deleting code to adding abstraction. Keep structs limited to fields
Martie uses. A `New...` function should validate, set defaults, or wire real
dependencies. Comments explain constraints and decisions, not syntax.

Test state transitions, idempotency, configuration validation, and protocol
edges. Do not add tests for trivial wiring. Run `gofmt` on changed Go files,
then `go test ./...` and `go vet ./...`. If the normal Go cache is unavailable,
use `GOCACHE="$PWD/.gocache"`.
