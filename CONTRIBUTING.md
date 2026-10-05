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
- `gen/` is committed; regenerate with `make proto` after editing `proto/`.
- Run `go test ./... -race` before committing.
