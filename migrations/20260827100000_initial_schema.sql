-- Initial schema: client api keys, the model registry and routing rules,
-- encrypted per-client vendor keys, and the config version replicas poll.
CREATE TABLE IF NOT EXISTS api_key (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    key_lookup TEXT NOT NULL UNIQUE, -- sha256 hex, finds the row
    key_hash TEXT NOT NULL,          -- sha256 hex, authenticates it
    scopes TEXT[] NOT NULL,
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model (
    id TEXT PRIMARY KEY,
    vendor TEXT NOT NULL,
    endpoint TEXT NOT NULL DEFAULT '',
    efforts TEXT[] NOT NULL,
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    rpm INT NOT NULL DEFAULT 0,
    price_in_per_mtok NUMERIC NOT NULL DEFAULT 0,
    price_out_per_mtok NUMERIC NOT NULL DEFAULT 0,
    daily_tokens_per_key BIGINT NOT NULL DEFAULT 0,
    daily_tokens_per_user BIGINT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rule (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    priority INT NOT NULL UNIQUE,
    effort TEXT NOT NULL DEFAULT '', -- '' = any effort
    attributes JSONB NOT NULL DEFAULT '{}', -- equality predicates
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rule_model (
    rule_id UUID NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
    position INT NOT NULL,
    model_id TEXT NOT NULL REFERENCES model(id),
    PRIMARY KEY (rule_id, position)
);

CREATE TABLE IF NOT EXISTS vendor_key (
    id UUID PRIMARY KEY,
    api_key_id UUID NOT NULL REFERENCES api_key(id),
    vendor TEXT NOT NULL,
    key_ciphertext BYTEA NOT NULL, -- AES-256-GCM, nonce||ciphertext
    key_version INT NOT NULL,      -- keyring version that sealed it
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (api_key_id, vendor)
);

-- Single-row version counter bumped by every admin mutation; replicas poll it.
CREATE TABLE IF NOT EXISTS config_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO config_state (id, version) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;

