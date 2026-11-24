-- +goose Up
CREATE TABLE settlements
(
    id           UUID PRIMARY KEY     DEFAULT gen_random_uuid(),
    group_id     UUID        NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    from_user    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_user      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount BIGINT      NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_user <> to_user)
);

CREATE INDEX idx_settlements_group_id ON settlements (group_id);

-- +goose Down
DROP TABLE IF EXISTS settlements;