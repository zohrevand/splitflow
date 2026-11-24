package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zohrevand/splitflow/internal/store/db"
)

type Store struct {
	*db.Queries
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		Queries: db.New(pool),
		pool:    pool,
	}
}

// ExecTx runs fn inside a PostgreSQL transaction.
// If fn returns an error, the transaction is rolled back automatically.
func (s *Store) ExecTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Rollback is a safe no-op if Commit succeeds
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.WithTx(tx)
	if err := fn(qtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
