package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/zohrevand/splitflow/internal/domain"
	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
)

type SettlementHandler struct {
	store *store.Store
}

func NewSettlementHandler(s *store.Store) *SettlementHandler {
	return &SettlementHandler{store: s}
}

type createSettlementRequest struct {
	ToUserID string `json:"to_user_id"`
	Amount   int64  `json:"amount"`
}

// CreateSettlement records that the authenticated user paid ToUserID.
func (h *SettlementHandler) CreateSettlement(w http.ResponseWriter, r *http.Request) {
	fromUUID, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	var req createSettlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	toUUID, err := parseUUID(req.ToUserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to_user_id")
		return
	}
	if fromUUID == toUUID {
		writeError(w, http.StatusBadRequest, "cannot settle with yourself")
		return
	}

	toMember, err := h.store.IsGroupMember(r.Context(), db.IsGroupMemberParams{
		GroupID: groupUUID,
		UserID:  toUUID,
	})
	if err != nil || !toMember {
		writeError(w, http.StatusBadRequest, "recipient is not a member of this group")
		return
	}

	settlement, err := h.store.CreateSettlement(r.Context(), db.CreateSettlementParams{
		GroupID:  groupUUID,
		FromUser: fromUUID,
		ToUser:   toUUID,
		Amount:   req.Amount,
	})
	if err != nil {
		slog.Error("failed to create settlement", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, settlement)
}

// GetSettlementPlan returns the simplified list of transfers needed to settle the group.
func (h *SettlementHandler) GetSettlementPlan(w http.ResponseWriter, r *http.Request) {
	_, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	rows, err := h.store.GetGroupBalances(r.Context(), groupUUID)
	if err != nil {
		slog.Error("failed to get balances", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	balances := make([]domain.Balance, len(rows))
	for i, row := range rows {
		balances[i] = domain.Balance{
			UserID:          row.UserID.String(),
			Name:            row.Name,
			NetBalanceCents: row.NetBalanceCents,
		}
	}

	transfers := domain.SimplifyDebts(balances)
	writeJSON(w, http.StatusOK, map[string]any{
		"transfers": transfers,
	})
}
