-- +goose Up
ALTER TABLE expenses
    ADD COLUMN idempotency_key TEXT;

-- Prevent duplicate expenses for the same group + idempotency key
CREATE UNIQUE INDEX idx_expenses_group_idempotency
    ON expenses (group_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE expense_splits
(
    expense_id UUID   NOT NULL REFERENCES expenses (id) ON DELETE CASCADE,
    user_id    UUID   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    owed_cents BIGINT NOT NULL CHECK (owed_cents >= 0),
    PRIMARY KEY (expense_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS expense_splits;
DROP INDEX IF EXISTS idx_expenses_group_idempotency;
ALTER TABLE expenses
    DROP COLUMN IF EXISTS idempotency_key;