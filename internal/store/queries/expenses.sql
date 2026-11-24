-- name: IsGroupMember :one
SELECT EXISTS (SELECT 1
               FROM group_members
               WHERE group_id = $1
                 AND user_id = $2);

-- name: ListGroupMemberIDs :many
SELECT user_id
FROM group_members
WHERE group_id = $1
ORDER BY user_id ASC;

-- name: GetExpenseByIdempotencyKey :one
SELECT id,
       group_id,
       paid_by,
       description,
       amount,
       currency,
       idempotency_key,
       created_at
FROM expenses
WHERE group_id = $1
  AND idempotency_key = $2;

-- name: CreateExpense :one
INSERT INTO expenses (group_id, paid_by, description, amount, currency, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, group_id, paid_by, description, amount, currency, idempotency_key, created_at;

-- name: CreateExpenseSplit :exec
INSERT INTO expense_splits (expense_id, user_id, owed_cents)
VALUES ($1, $2, $3);

-- name: GetGroupBalances :many
WITH paid AS (SELECT paid_by AS user_id, COALESCE(SUM(amount), 0)::BIGINT AS total_paid
              FROM expenses
              WHERE group_id = $1
              GROUP BY paid_by),
     owed AS (SELECT es.user_id, COALESCE(SUM(es.owed_cents), 0)::BIGINT AS total_owed
              FROM expense_splits es
                       INNER JOIN expenses e ON e.id = es.expense_id
              WHERE e.group_id = $1
              GROUP BY es.user_id),
     settled_out AS (SELECT from_user AS user_id, COALESCE(SUM(amount), 0)::BIGINT AS total_out
                     FROM settlements
                     WHERE group_id = $1
                     GROUP BY from_user),
     settled_in AS (SELECT to_user AS user_id, COALESCE(SUM(amount), 0)::BIGINT AS total_in
                    FROM settlements
                    WHERE group_id = $1
                    GROUP BY to_user)
SELECT u.id          AS user_id,
       u.name,
       u.email,
       (
           COALESCE(p.total_paid, 0)
               - COALESCE(o.total_owed, 0)
               + COALESCE(so.total_out, 0)
               - COALESCE(si.total_in, 0)
           )::BIGINT AS net_balance_cents
FROM group_members gm
         INNER JOIN users u ON u.id = gm.user_id
         LEFT JOIN paid p ON p.user_id = gm.user_id
         LEFT JOIN owed o ON o.user_id = gm.user_id
         LEFT JOIN settled_out so ON so.user_id = gm.user_id
         LEFT JOIN settled_in si ON si.user_id = gm.user_id
WHERE gm.group_id = $1
ORDER BY net_balance_cents DESC;

-- name: ListGroupExpenses :many
SELECT id, group_id, paid_by, description, amount, currency, created_at
FROM expenses
WHERE group_id = $1
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at DESC, id DESC
LIMIT $2;