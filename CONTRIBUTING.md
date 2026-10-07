# llm-proxy

Central LLM gateway: holds all vendor API keys, routes GenerateText requests by
effort + attributes rules, throttles by RPM and daily token budgets, reports usage to
a usage sink.

## Contribution rules

- Keep comments to 3 lines max — normally one line, ideally inline (`// ...` on the
  code line). Comments explain *why*, never *what*. Delete redundant comments instead
  of restating the code.
- Never log, echo, or write a vendor LLM key, client API key, or the encryption
  keyring to stdout, Sentry, an error message, or a gRPC response. Vendor keys exist
  in plaintext only inside the in-memory registry snapshot.
- Look up by key ID, not key name. Vendor keys, adapters and anything stored
  per client are looked up by `APIKeyID`. `KeyName` is only a label, used for
  budgets, logs and usage. Tests must give the ID and the name different
  values so a mix-up fails the test.
- Guard state changes in SQL. Every `UPDATE` that changes a row's state
  includes the states it may move from (`AND state IN (…)`) and checks the
  number of rows affected. A row in a final state never changes.
- Commit before side effects. Usage events, budget charges and other outside
  effects happen only after the DB transaction commits, and are written so
  that repeating them doesn't charge twice.
- Clean up vendor work when the DB write fails. When a vendor job is created
  before the DB row, a failed or losing insert must cancel that vendor job, or
  a pending row is written first and then filled in.
- Don't throw away errors when writing data. `_ =` and `_, _ =` are not allowed
  on DB writes or vendor calls. At minimum, log the error with the batch or
  request ID.
- Config names match the docs. Every env var is spelled the same in
  `config.go`, `env.example` and the README. Add a test that compares them.
- Every response field is filled or marked `reserved`. Don't add a proto
  field the server never sets.
- One error format. Per-item errors always include `code`, `reason` and
  `message`, built by one shared helper with codes from
  `google.golang.org/grpc/codes`.
- One helper for user identity. Budget checks, budget charges and usage
  events all get the user ID from the same helper (including the `svc:<name>`
  fallback).
- Map vendor states carefully. In-between vendor states such as `CANCELLING`
  or `UPDATING` map to an unfinished state, never to a final one.
- `gen/` is committed; regenerate with `make proto` after editing `proto/`.
- Run `go test ./... -race` before committing.
