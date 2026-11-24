-- name: CreateSettlement :one
INSERT INTO settlements (group_id, from_user, to_user, amount)
VALUES ($1, $2, $3, $4)
RETURNING id, group_id, from_user, to_user, amount, created_at;

-- name: ListGroupSettlements :many
SELECT id, group_id, from_user, to_user, amount, created_at
FROM settlements
WHERE group_id = $1
ORDER BY created_at DESC;