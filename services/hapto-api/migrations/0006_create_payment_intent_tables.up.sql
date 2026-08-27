-- Payment settlement needs to resolve "the" wallet for a (user, currency)
-- pair unambiguously (internal/ledger.GetWalletByUserID). The original
-- wallets table (0004) had no such constraint; add it here since this is
-- the first migration that actually depends on it.
ALTER TABLE wallets ADD CONSTRAINT wallets_user_id_currency_key UNIQUE (user_id, currency);

CREATE TABLE payment_intents (
    id UUID PRIMARY KEY,
    merchant_user_id UUID NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    status TEXT NOT NULL,
    nonce BYTEA,
    idempotency_key TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_payment_intents_merchant_user_id ON payment_intents (merchant_user_id);
CREATE INDEX idx_payment_intents_status_expires_at ON payment_intents (status, expires_at);

CREATE TABLE payment_authorizations (
    id UUID PRIMARY KEY,
    payment_intent_id UUID NOT NULL UNIQUE REFERENCES payment_intents(id),
    customer_signing_device_id UUID NOT NULL,
    signature BYTEA NOT NULL,
    signed_payload_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- payment_intent_id is UNIQUE above: at most one authorization can ever
-- exist per intent. That's the replay guard for a given (payment_intent_id,
-- nonce) pair, since each intent has exactly one nonce for its lifetime.

CREATE TABLE ble_sessions (
    id UUID PRIMARY KEY,
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),
    merchant_device_id UUID NOT NULL,
    customer_device_id UUID NOT NULL,
    session_id TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    status TEXT NOT NULL
);

CREATE INDEX idx_ble_sessions_payment_intent_id ON ble_sessions (payment_intent_id);
