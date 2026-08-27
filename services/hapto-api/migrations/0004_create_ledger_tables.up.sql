CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    currency TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE ledger_transactions (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    direction TEXT NOT NULL CHECK (direction IN ('debit', 'credit')),
    transaction_id UUID NOT NULL REFERENCES ledger_transactions(id),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transaction_id ON ledger_entries (transaction_id);
