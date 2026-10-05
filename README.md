# llm-proxy

[![Test](https://github.com/ai-process/llm-proxy/actions/workflows/test.yml/badge.svg)](https://github.com/ai-process/llm-proxy/actions/workflows/test.yml)
[![Docker image](https://img.shields.io/docker/v/coyl/llm-proxy?label=docker%20hub&sort=semver)](https://hub.docker.com/r/coyl/llm-proxy)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A self-hosted LLM gateway. Your services talk to one gRPC endpoint; the proxy holds
the vendor API keys, decides which model answers each request, falls back when a
vendor is down, enforces rate and spend limits, and records usage.

- **One place for vendor keys.** OpenAI, Google Gemini, DeepSeek and TypeSafe credentials
  live encrypted in Postgres (AES-256-GCM). Client services never see them.
- **Routing by rules, not code.** Clients send an *effort* (`LOW` / `MEDIUM` / `HIGH`) and
  free-form *attributes*; an ordered rule list maps them to a model chain with fallbacks.
  Rules are changed at runtime over an admin RPC, with no redeploy.
- **Throttling.** Per-model requests-per-minute and daily token budgets, per API key and
  per end user, backed by Redis.
- **More than text.** Text generation with structured (JSON-schema) output, speech
  synthesis, image generation and typed judgments.
- **Usage reporting.** Optionally forwards one event per call to a gRPC usage sink.

## Contents

- [How it works](#how-it-works)
- [Quick start (Docker Compose)](#quick-start-docker-compose)
- [Installation](#installation)
  - [Docker image](#docker-image)
  - [Building from source](#building-from-source)
  - [Production checklist](#production-checklist)
- [Configuration reference](#configuration-reference)
- [Setting it up: keys, models, rules](#setting-it-up-keys-models-rules)
- [Calling the proxy](#calling-the-proxy)
- [Concepts](#concepts)
- [Error contract](#error-contract)
- [Operations](#operations)
- [Development](#development)
- [License](#license)

## How it works

```
 your services ──gRPC (Bearer llm_…)──▶ llm-proxy ──▶ OpenAI / Gemini / DeepSeek / TypeSafe
                                          │  │
                                 Postgres ┘  └ Redis (limits)      └─▶ usage sink (optional)
                                 config, keys, encrypted vendor keys
```

Two gRPC services share port `9090`:

| Service | Purpose | Needed scope |
| --- | --- | --- |
| `llmproxy.v1.LLMProxyService` | `GenerateText`, `SynthesizeSpeech`, `GenerateImage`, `Judge`, `ListModels` | `generate` |
| `llmproxy.v1.LLMProxyAdminService` | models, rules, vendor keys, API keys, key rotation | `admin` |

Port `8080` serves plain HTTP health probes (`/healthz`, `/readyz`). gRPC server
reflection is on, so `grpcurl` works without the `.proto` files.

## Quick start (Docker Compose)

Requirements: Docker with the Compose plugin, and [`grpcurl`](https://github.com/fullstorydev/grpcurl).

```bash
git clone https://github.com/ai-process/llm-proxy.git
cd llm-proxy
docker compose up --build -d
curl -s localhost:8080/readyz     # {"status":"ready"}
```

This starts the proxy, Postgres 16 and Redis 7. The compose file ships a **development-only**
encryption key and the bootstrap admin key `dev-bootstrap`; never reuse them anywhere real.
The database schema applies itself on first start.

Mint a client key, give it a vendor credential, register a model and a catch-all rule:

```bash
export ADMIN="authorization: Bearer dev-bootstrap"
G="grpcurl -plaintext -H"

# 1. A client API key (the plaintext is shown exactly once).
$G "$ADMIN" -d '{"name":"my-app","scopes":["generate"]}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/MintAPIKey

# 2. Your OpenAI key, assigned to that client key.
$G "$ADMIN" -d '{"api_key_name":"my-app","vendor":"openai","plain_key":"sk-..."}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/SetVendorKey

# 3. A model, and 4. rules (one catch-all per effort is mandatory).
$G "$ADMIN" -d '{"model":{"id":"gpt-4o-mini","vendor":"openai",
  "efforts":["EFFORT_LOW","EFFORT_MEDIUM","EFFORT_HIGH"],
  "price_in_per_mtok":0.15,"price_out_per_mtok":0.60,"enabled":true}}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/UpsertModel

$G "$ADMIN" -d '{"rules":[
  {"name":"default","priority":1,"use":["gpt-4o-mini"],"enabled":true}]}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/ReplaceRules

# 5. Ask something with the key from step 1.
$G "authorization: Bearer llm_<your key>" -d '{
  "effort":"EFFORT_LOW",
  "messages":[{"role":"MESSAGE_ROLE_USER","text":"Say hi in three words."}]}' \
  localhost:9090 llmproxy.v1.LLMProxyService/GenerateText
```

The response names the model and rule that served it (`resolved_model`, `matched_rule`).
Once your real keys exist, unset `BOOTSTRAP_ADMIN_KEY`.

## Installation

### Docker image

Release images are published to Docker Hub for `linux/amd64` and `linux/arm64`:

```bash
docker pull coyl/llm-proxy:latest      # or pin a release tag, e.g. coyl/llm-proxy:0.1.0
```

The image is a static Go binary on Alpine plus `ffmpeg` (needed to turn Gemini's raw
PCM speech into OGG/Opus). It runs as a non-root user, exposes `8080` (HTTP) and
`9090` (gRPC) and is configured entirely through environment variables.

You need a Postgres database (required; 16 is what the compose file uses) and a Redis (optional, see below):

```bash
# generate the at-rest encryption keyring once and keep it safe
export LLMPROXY_ENCRYPTION_KEYS="1:$(head -c 32 /dev/urandom | base64)"
export BOOTSTRAP_ADMIN_KEY="$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"

docker run -d --name llm-proxy \
  -p 9090:9090 -p 8080:8080 \
  -e DATABASE_URL='postgres://user:pass@db-host:5432/llmproxy?sslmode=require' \
  -e LLMPROXY_ENCRYPTION_KEYS \
  -e BOOTSTRAP_ADMIN_KEY \
  -e REDIS_ADDR=redis-host:6379 \
  coyl/llm-proxy:latest
```

A minimal Compose service for your own stack:

```yaml
services:
  llm-proxy:
    image: coyl/llm-proxy:latest
    restart: unless-stopped
    environment:
      DATABASE_URL: postgres://llmproxy:${DB_PASSWORD}@db:5432/llmproxy?sslmode=disable
      LLMPROXY_ENCRYPTION_KEYS: ${LLMPROXY_ENCRYPTION_KEYS}
      REDIS_ADDR: redis:6379
    ports:
      - "9090:9090"
    depends_on: [db, redis]
```

On Kubernetes, point the liveness probe at `GET :8080/healthz` and the readiness probe
at `GET :8080/readyz` (checks the database). Run as many replicas as you like; they share
Postgres and Redis and pick up configuration changes within `ROUTING_RELOAD_SECONDS`.

### Building from source

Requires Go 1.25+ (and `ffmpeg` on `PATH` if you use speech).

```bash
make build                       # → bin/llm-proxy-server
export DATABASE_URL=postgres://llmproxy:llmproxy@localhost:5432/llmproxy?sslmode=disable
export LLMPROXY_ENCRYPTION_KEYS="1:$(head -c 32 /dev/urandom | base64)"
./bin/llm-proxy-server
```

Or build the image yourself: `docker build -t llm-proxy .`

### Production checklist

- **TLS.** The server speaks plaintext gRPC. Terminate TLS in front of it (Envoy, nginx
  with `grpc_pass`, a service mesh, a cloud load balancer) or keep it on a private network.
- **Back up the encryption keyring.** Without `LLMPROXY_ENCRYPTION_KEYS` the stored vendor
  keys are unreadable. Treat it like any other root secret.
- **Mint real keys, then unset `BOOTSTRAP_ADMIN_KEY`.** It is accepted as an implicit admin key.
- **Run Redis if you want limits.** With `REDIS_ADDR` empty, RPM and token budgets are
  **not enforced** (the proxy fails open by design, and does the same if Redis errors).
- **Give each client its own API key** (and its own vendor credentials), so spend, models
  and revocation are per client.
- **Mind your Postgres connection count.** Each replica holds up to `DB_MAX_CONNS`
  (default 25) connections.
- **Back up Postgres.** All configuration lives there.

## Configuration reference

Everything is an environment variable; see [`env.example`](env.example).

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | – | **Required.** Postgres connection string. |
| `LLMPROXY_ENCRYPTION_KEYS` | – | **Required.** Versioned keyring `1:<base64 32 bytes>[,2:…]`. The highest version encrypts new writes; all listed versions can decrypt. |
| `GRPC_ADDR` | `:9090` | gRPC listen address (data and admin planes). |
| `HTTP_ADDR` | `:8080` | Health-probe listen address. |
| `LOG_LEVEL` | `info` | zerolog level. |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful drain time on SIGTERM. |
| `DB_MAX_CONNS` / `DB_MIN_CONNS` | `25` / `5` | Postgres pool bounds. |
| `ROUTING_RELOAD_SECONDS` | `60` | How often replicas poll for config changed elsewhere. |
| `AUTO_MIGRATE` | `true` | Apply embedded migrations at startup (guarded by an advisory lock, safe for rolling deploys). Off means applying `migrations/*.sql` by hand. |
| `MIGRATE_TIMEOUT` | `3m` | Bound for the whole migration run. |
| `REDIS_ADDR` | empty | Redis for throttling. Empty disables limits. |
| `REDIS_PASSWORD` / `REDIS_DB` | empty / `0` | Redis auth and database. |
| `REDIS_KEY_PREFIX` | empty | Namespace for counters when Redis is shared (compose uses `dev`). |
| `BOOTSTRAP_ADMIN_KEY` | empty | Implicit admin key for creating the first real key. Unset afterwards. |
| `USAGE_COLLECTOR_HOST` | empty | `host:port` of a gRPC usage sink. Empty disables reporting. |
| `USAGE_PROJECT_ID` | `llm-proxy` | Default project name on usage events. |
| `SENTRY_DSN` | empty | Error reporting; empty disables it. |
| `SENTRY_ENVIRONMENT` / `SENTRY_RELEASE` | `development` / empty | Sentry metadata. |
| `SENTRY_ENABLE_TRACING` | `false` | Sentry performance tracing. |

## Setting it up: keys, models, rules

All of this is done with the admin service (`LLMProxyAdminService`) using a key that has
the `admin` scope. Mutations are validated in a transaction together with the existing
config, so an invalid state can never become active.

### 1. API keys

```bash
grpcurl -plaintext -H "authorization: Bearer $ADMIN_KEY" \
  -d '{"name":"billing-service","scopes":["generate"]}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/MintAPIKey
```

Keys look like `llm_<hex>`. Only a hash is stored, and the plaintext is returned once.
Scopes: `generate` (data plane) and `admin` (implies everything).
`ListAPIKeys` and `RevokeAPIKey` do what they say.

### 2. Vendor keys (per client)

A vendor credential is assigned to **one** API key and encrypted at rest. A client can
only use models of vendors it holds a credential for, and `ListModels` reflects that.

```bash
grpcurl -plaintext -H "authorization: Bearer $ADMIN_KEY" \
  -d '{"api_key_name":"billing-service","vendor":"google","plain_key":"AIza..."}' \
  localhost:9090 llmproxy.v1.LLMProxyAdminService/SetVendorKey
```

Vendors: `openai`, `google`, `deepseek` (OpenAI-compatible API, default endpoint
`https://api.deepseek.com`) and `typesafe` (judgments only). Use `DeleteVendorKey` to remove one.

### 3. Models

```json
{ "model": {
    "id": "gemini-2.5-flash",              // referenced by rules and model_override; also sent to the vendor
    "vendor": "google",
    "endpoint": "",                         // optional base-URL override (Azure, self-hosted gateways…)
    "efforts": ["EFFORT_LOW", "EFFORT_MEDIUM"],
    "capabilities": ["google_search"],
    "rpm": 600,                             // per client key per minute, 0 = unlimited
    "price_in_per_mtok": 0.30,              // for usage reporting
    "price_out_per_mtok": 2.50,
    "price_in_peak_per_mtok": 0,            // for vendors with peak-hour pricing, else 0
    "price_out_peak_per_mtok": 0,
    "daily_tokens_per_key": 50000000,       // per API key per UTC day, 0 = unlimited
    "daily_tokens_per_user": 500000,        // per attributes["user_id"] per UTC day
    "enabled": true } }
```

(The `//` comments are for illustration; strip them from real JSON.) Send it to `UpsertModel`.

Capabilities a model may declare:

| Capability | Meaning |
| --- | --- |
| `google_search` | Can serve requests with `enable_google_search`. |
| `no_structured_output` | The vendor API can't take a JSON schema (e.g. DeepSeek). The shape is requested in the prompt and verified by the proxy instead. |
| `tts` | Can serve `SynthesizeSpeech`. |
| `image` | Can serve `GenerateImage`. |
| `judge` | Can serve `Judge` (TypeSafe models). |

### 4. Rules

```bash
grpcurl -plaintext -H "authorization: Bearer $ADMIN_KEY" -d '{"rules":[
  {"name":"spanish-high",  "priority":30, "effort":"EFFORT_HIGH",
   "attributes":{"language":"es"}, "use":["gemini-2.5-pro","gpt-4o"], "enabled":true},
  {"name":"low-default",   "priority":20, "effort":"EFFORT_LOW",
   "use":["deepseek-chat","gpt-4o-mini"], "enabled":true},
  {"name":"anything-else", "priority":1,
   "use":["gpt-4o-mini"], "enabled":true}
]}' localhost:9090 llmproxy.v1.LLMProxyAdminService/ReplaceRules
```

- The **highest priority** matching rule wins; priorities and names are unique.
- A rule matches when its `effort` equals the request's (unspecified = any effort) **and**
  every attribute in the rule equals the request's attribute.
- `use` is the ordered **fallback chain**, cheaper-first by convention. If the first model
  is throttled or its vendor is unavailable, the next is tried.
- Validation requires a **catch-all** (no attribute predicates) for every effort, so no
  request can ever fall through. `anything-else` above covers all three at once.
- `ReplaceRules` swaps the whole set atomically; there is no per-rule CRUD.

Inspect the live configuration with `GetConfig`.

### Speech, image and judgment rules

These modalities carry no effort. The proxy routes them at `EFFORT_LOW` with a reserved
`attributes["modality"]` set to `tts`, `image` or `judge`, and only rules that name that
modality can match, over models that declare the matching capability:

```json
{"name":"speech", "priority":100, "attributes":{"modality":"tts"},
 "use":["gemini-2.5-flash-preview-tts"], "enabled":true}
```

Without such a rule the call fails with `FAILED_PRECONDITION no_capable_model`.

### Rotating the encryption key

Add a higher version to the keyring (`1:…,2:…`), redeploy, call `RotateEncryption` to
re-encrypt every stored vendor key, then drop the old version.

## Calling the proxy

Authenticate every call with `authorization: Bearer llm_…` gRPC metadata. Generate Go,
Python, TypeScript… clients from [`proto/llmproxy/v1`](proto/llmproxy/v1) with `buf` or
`protoc`, or use the committed Go stubs in [`gen/llmproxy/v1`](gen/llmproxy/v1)
(`github.com/ai-process/llm-proxy/gen/llmproxy/v1`).

### GenerateText

```json
{
  "effort": "EFFORT_MEDIUM",
  "attributes": {"language": "es", "user_id": "u-42", "task": "summary"},
  "base_system_instruction": "You are a concise assistant.",
  "request_instruction": "Answer in one paragraph.",
  "messages": [{"role": "MESSAGE_ROLE_USER", "text": "Summarize …"}],
  "response_schema": {"type": "object",
    "properties": {"summary": {"type": "string"}}, "required": ["summary"]},
  "max_output_tokens": 800,
  "action_id": "app.summarize"
}
```

| Field | Notes |
| --- | --- |
| `effort` | Routing tier. |
| `attributes` | Opaque key/values matched by rules and copied to usage events. `user_id` charges the per-user daily budget. `usage_project` overrides the usage project (see below). |
| `base_system_instruction`, `request_instruction` | System content never travels as a message. |
| `response_schema` | Unset = free text. Set = JSON output validated against the schema; a vendor that can't enforce it is verified by the proxy. |
| `max_output_tokens` | Ceiling, `0` = none. Output tokens are the expensive ones; set it. |
| `enable_google_search` | Only Google models declaring `google_search` can serve it. |
| `action_id` | Free-form label for usage attribution. |
| `model_override` | Exact model id; bypasses rules but **not** throttles. |

The response carries `choices`, token `usage`, `resolved_model`, `resolved_vendor` and
`matched_rule` (empty on override).

### SynthesizeSpeech, GenerateImage

`SynthesizeSpeech` takes `text` (read verbatim) and an optional `language` and returns OGG/Opus
`audio`. `GenerateImage` takes a `prompt` and returns `image` bytes with a `mime_type`.
Both accept `attributes`, `action_id` and `model_override`, and need the rules described
[above](#speech-image-and-judgment-rules).

### Judge

`Judge` answers typed questions about one state instead of writing text, so you get
calibrated probabilities to branch on rather than prose to parse. All questions in a call
are answered against the same state in parallel.

| Type | Question | Answer |
| --- | --- | --- |
| `JUDGE_TYPE_NOUL` | Does this hold? | probability of yes (`noul`, 0–1) |
| `JUDGE_TYPE_CHOICE` | Which one? | `choice` plus full `probabilities` and `confidence` |
| `JUDGE_TYPE_SCORE` | How much? | probability-weighted position on ordered `levels` |

```json
{
  "state_json": "{\"message\":\"My card was charged twice!\"}",
  "questions": {
    "angry": {"type": "JUDGE_TYPE_NOUL", "instructions": "Is the author upset?"},
    "topic": {"type": "JUDGE_TYPE_CHOICE", "instructions": "What is this about?",
              "options": {"billing": "payments and charges", "bug": "software defects"}}
  }
}
```

The vendor is [TypeSafe](https://typesafe.ai) (`jev-latest`), which bills input only and
writes no text, so its models must declare the `judge` capability and refuse `GenerateText`.

### ListModels

Returns the models the calling key can use (those whose vendor it holds a credential for), with
efforts, capabilities, rpm and prices.

## Concepts

**Effort** is the only routing field the proxy understands; everything else about a request's
purpose travels in free-form attributes, so the proxy knows nothing about your product.

**Throttling** has two layers per model: a fixed per-minute request window
(`rpm`, per API key) and daily token budgets (`daily_tokens_per_key`, `daily_tokens_per_user`,
UTC day). A throttled model is skipped in the fallback chain; if none of the chain is
available the caller gets `RESOURCE_EXHAUSTED`.

**Usage reporting.** When `USAGE_COLLECTOR_HOST` is set, every call emits an event (vendor,
model, tokens, duration, status, `action_id`, rule name and your attributes as metadata) to a
gRPC service implementing [`usage-proto/usage/v1/usage.proto`](usage-proto/usage/v1/usage.proto).
Events are attributed to the project named by the `usage_project` attribute, falling back to the
API key's name. Any service implementing `RecordUsageEvent(s)` works as a sink.

## Error contract

| gRPC status | Reason | What to do |
| --- | --- | --- |
| `UNAVAILABLE` | `vendors_unavailable` | The whole chain failed. **The only retryable error.** |
| `RESOURCE_EXHAUSTED` | `throttled:rpm`, `throttled:budget_key`, `throttled:budget_user` | Back off; don't hot-retry. |
| `FAILED_PRECONDITION` | `output_truncated` | Raise `max_output_tokens`; don't retry as-is. |
| `FAILED_PRECONDITION` | `no_capable_model` | No model in the matched chain can serve this request/modality. |
| `FAILED_PRECONDITION` | `no_vendor_key` | The client key holds no credential for the chain's vendors. |
| `FAILED_PRECONDITION` | `not_configured` | No models/rules configured yet. |
| `INTERNAL` | `vendor_error` | Terminal vendor failure. |
| `UNAUTHENTICATED` / `PERMISSION_DENIED` | – | Missing/invalid key, or key lacks the scope. |

## Operations

- **Health:** `GET :8080/healthz` (process up) and `GET :8080/readyz` (database reachable).
- **Logs:** structured JSON via zerolog. Vendor keys, client keys and the keyring are never logged.
- **Config changes** are picked up by all replicas within `ROUTING_RELOAD_SECONDS`; the replica that
  handled the admin call applies them immediately.
- **Upgrades:** migrations run at startup under a database advisory lock, so rolling deploys are
  safe. Roll forward by deploying a newer image tag.
- **Graceful shutdown:** on SIGTERM in-flight calls get `SHUTDOWN_TIMEOUT` to finish.

## Development

```bash
make test       # go test -race ./...
make build      # binary in bin/
make proto      # regenerate gen/ after editing proto/ (needs buf)
make clients    # regenerate the usage-sink stubs from usage-proto/
docker compose up --build   # local stack with Postgres + Redis
```

Layout: `cmd/server` (entrypoint), `internal/router` (rule matching, fallback, admission),
`internal/registry` (validated config snapshots), `internal/llm` (vendor-neutral chat model and
adapters), `internal/throttle` (Redis limits), `internal/grpcapi` (RPC handlers),
`internal/crypto` (keyring), `migrations/` (embedded SQL).

Pull requests are welcome; read [`CONTRIBUTING.md`](CONTRIBUTING.md) first.

### Releasing

Pushing a git tag builds and publishes a multi-arch image to Docker Hub as
`coyl/llm-proxy:<tag>` (and `latest` for semver tags). The repository needs the secrets
`DOCKER_USERNAME` and `DOCKER_PASSWORD` (a Docker Hub access token).

```bash
git tag 0.1.0 && git push origin 0.1.0
```

## License

[MIT](LICENSE)
