-- PostgreSQL Serializable Double-Entry Ledger Schema
-- Enforces ISO-4217 BIGINT minor units and strict append-only immutability.

CREATE TABLE IF NOT EXISTS ledger_accounts (
    account_id VARCHAR(64) PRIMARY KEY,
    account_type VARCHAR(32) NOT NULL CHECK (account_type IN ('ASSET', 'LIABILITY', 'EQUITY', 'REVENUE', 'EXPENSE')),
    currency CHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS journal_transactions (
    transaction_id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    currency CHAR(3) NOT NULL,
    previous_hash CHAR(64) NOT NULL,
    entry_hash CHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ledger_lines (
    line_id BIGSERIAL PRIMARY KEY,
    transaction_id VARCHAR(64) NOT NULL REFERENCES journal_transactions(transaction_id) ON DELETE RESTRICT,
    account_id VARCHAR(64) NOT NULL REFERENCES ledger_accounts(account_id) ON DELETE RESTRICT,
    direction VARCHAR(6) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0), -- Strictly positive ISO-4217 minor units
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_lines_account_id ON ledger_lines(account_id);
CREATE INDEX idx_ledger_lines_transaction_id ON ledger_lines(transaction_id);

-- Immutability Trigger: Prevent UPDATE or DELETE on committed ledger records
CREATE OR REPLACE FUNCTION enforce_append_only_ledger()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger-vault: UPDATE and DELETE operations are strictly prohibited on immutable ledger tables';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_journal_transactions
BEFORE UPDATE OR DELETE ON journal_transactions
FOR EACH ROW EXECUTE FUNCTION enforce_append_only_ledger();

CREATE TRIGGER trg_immutable_ledger_lines
BEFORE UPDATE OR DELETE ON ledger_lines
FOR EACH ROW EXECUTE FUNCTION enforce_append_only_ledger();
