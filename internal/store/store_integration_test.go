package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
	"github.com/zohrevand/splitflow/migrations"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("splitflow_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pgContainer.Terminate(context.Background())
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pgxpool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Run all embedded goose migrations against the ephemeral Postgres container
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("failed to close sqlDB: %v", err)
		}
	}()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("goose up failed: %v", err)
	}

	return store.New(pool)
}

func TestLedgerAndSettlementsIntegration(t *testing.T) {
	st := setupTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create two users
	u1, err := st.CreateUser(ctx, db.CreateUserParams{
		Email: "hasan@test.com", Name: "Hasan", PasswordHash: "hash1",
	})
	if err != nil {
		t.Fatalf("CreateUser u1: %v", err)
	}
	u2, err := st.CreateUser(ctx, db.CreateUserParams{
		Email: "ali@test.com", Name: "Ali", PasswordHash: "hash2",
	})
	if err != nil {
		t.Fatalf("CreateUser u2: %v", err)
	}

	// 2. Create group + add both members atomically
	var groupID pgtype.UUID
	err = st.ExecTx(ctx, func(q *db.Queries) error {
		g, txErr := q.CreateGroup(ctx, db.CreateGroupParams{Name: "Trip", CreatedBy: u1.ID})
		if txErr != nil {
			return txErr
		}
		groupID = g.ID
		if txErr := q.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: g.ID, UserID: u1.ID}); txErr != nil {
			return txErr
		}
		return q.AddGroupMember(ctx, db.AddGroupMemberParams{GroupID: g.ID, UserID: u2.ID})
	})
	if err != nil {
		t.Fatalf("ExecTx create group: %v", err)
	}

	// 3. Hasan pays $50.00 (5000 cents), split 2500 each, with an Idempotency Key
	idemKey := pgtype.Text{String: "idem-key-1", Valid: true}
	exp, err := st.CreateExpense(ctx, db.CreateExpenseParams{
		GroupID:        groupID,
		PaidBy:         u1.ID,
		Description:    "Dinner",
		Amount:         5000,
		Currency:       "USD",
		IdempotencyKey: idemKey,
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if err := st.CreateExpenseSplit(ctx, db.CreateExpenseSplitParams{ExpenseID: exp.ID, UserID: u1.ID, OwedCents: 2500}); err != nil {
		t.Fatalf("CreateExpenseSplit u1: %v", err)
	}
	if err := st.CreateExpenseSplit(ctx, db.CreateExpenseSplitParams{ExpenseID: exp.ID, UserID: u2.ID, OwedCents: 2500}); err != nil {
		t.Fatalf("CreateExpenseSplit u2: %v", err)
	}

	// 4. Verify duplicate Idempotency Key is rejected by PostgreSQL
	_, dupErr := st.CreateExpense(ctx, db.CreateExpenseParams{
		GroupID:        groupID,
		PaidBy:         u1.ID,
		Description:    "Dinner Duplicate",
		Amount:         5000,
		Currency:       "USD",
		IdempotencyKey: idemKey,
	})
	if dupErr == nil {
		t.Fatalf("expected unique constraint error for duplicate idempotency_key, got nil")
	}

	// 5. Verify Balances: Hasan = +2500, Ali = -2500
	balances, err := st.GetGroupBalances(ctx, groupID)
	if err != nil {
		t.Fatalf("GetGroupBalances: %v", err)
	}
	if len(balances) != 2 || balances[0].NetBalanceCents != 2500 || balances[1].NetBalanceCents != -2500 {
		t.Fatalf("unexpected balances before settlement: %+v", balances)
	}

	// 6. Ali settles $25.00 with Hasan -> both balances must become 0
	_, err = st.CreateSettlement(ctx, db.CreateSettlementParams{
		GroupID:  groupID,
		FromUser: u2.ID,
		ToUser:   u1.ID,
		Amount:   2500,
	})
	if err != nil {
		t.Fatalf("CreateSettlement: %v", err)
	}

	settledBalances, err := st.GetGroupBalances(ctx, groupID)
	if err != nil {
		t.Fatalf("GetGroupBalances after settlement: %v", err)
	}
	for _, b := range settledBalances {
		if b.NetBalanceCents != 0 {
			t.Errorf("expected 0 balance for %s after settlement, got %d", b.Name, b.NetBalanceCents)
		}
	}
}
