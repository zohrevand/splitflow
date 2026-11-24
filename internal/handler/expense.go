package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zohrevand/splitflow/internal/domain"
	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
)

type ExpenseHandler struct {
	store *store.Store
}

func NewExpenseHandler(s *store.Store) *ExpenseHandler {
	return &ExpenseHandler{store: s}
}

type createExpenseRequest struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
}

func (h *ExpenseHandler) CreateExpense(w http.ResponseWriter, r *http.Request) {
	userUUID, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	// 1. Check Idempotency-Key header before doing any work
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey != "" {
		existing, err := h.store.GetExpenseByIdempotencyKey(r.Context(), db.GetExpenseByIdempotencyKeyParams{
			GroupID:        groupUUID,
			IdempotencyKey: pgtype.Text{String: idemKey, Valid: true},
		})
		if err == nil {
			writeJSON(w, http.StatusOK, existing)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("idempotency lookup failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	var req createExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "description and positive amount are required")
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	// 2. Transactionally create the expense and all member splits
	var created db.CreateExpenseRow
	err := h.store.ExecTx(r.Context(), func(q *db.Queries) error {
		memberIDs, txErr := q.ListGroupMemberIDs(r.Context(), groupUUID)
		if txErr != nil {
			return txErr
		}

		shares, txErr := domain.SplitEvenly(req.Amount, len(memberIDs))
		if txErr != nil {
			return txErr
		}

		exp, txErr := q.CreateExpense(r.Context(), db.CreateExpenseParams{
			GroupID:        groupUUID,
			PaidBy:         userUUID,
			Description:    req.Description,
			Amount:         req.Amount,
			Currency:       req.Currency,
			IdempotencyKey: pgtype.Text{String: idemKey, Valid: idemKey != ""},
		})
		if txErr != nil {
			return txErr
		}

		for i, memberID := range memberIDs {
			if txErr := q.CreateExpenseSplit(r.Context(), db.CreateExpenseSplitParams{
				ExpenseID: exp.ID,
				UserID:    memberID,
				OwedCents: shares[i],
			}); txErr != nil {
				return txErr
			}
		}

		created = exp
		return nil
	})
	if err != nil {
		slog.Error("failed to create expense", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *ExpenseHandler) GetBalances(w http.ResponseWriter, r *http.Request) {
	_, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	balances, err := h.store.GetGroupBalances(r.Context(), groupUUID)
	if err != nil {
		slog.Error("failed to calculate balances", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, balances)
}

type listExpensesResponse struct {
	Items      []db.ListGroupExpensesRow `json:"items"`
	NextCursor string                    `json:"next_cursor,omitempty"`
	HasMore    bool                      `json:"has_more"`
}

func (h *ExpenseHandler) ListExpenses(w http.ResponseWriter, r *http.Request) {
	_, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	limit := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > 50 {
			parsed = 50
		}
		limit = parsed
	}

	params := db.ListGroupExpensesParams{
		GroupID: groupUUID,
		Limit:   int32(limit + 1),
	}

	if cursorStr := r.URL.Query().Get("cursor"); cursorStr != "" {
		ts, id, err := decodeCursor(cursorStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		params.CursorCreatedAt = pgtype.Timestamptz{Time: ts, Valid: true}
		params.CursorID = id
	}

	rows, err := h.store.ListGroupExpenses(r.Context(), params)
	if err != nil {
		slog.Error("failed to list expenses", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	var nextCursor string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCursor(last.CreatedAt.Time, last.ID)
	}

	writeJSON(w, http.StatusOK, listExpensesResponse{
		Items:      rows,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func encodeCursor(createdAt time.Time, id pgtype.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, pgtype.UUID, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 2 {
		return time.Time{}, pgtype.UUID{}, errors.New("malformed cursor")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	id, err := parseUUID(parts[1])
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	return ts, id, nil
}
