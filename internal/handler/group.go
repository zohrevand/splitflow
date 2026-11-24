package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
)

type GroupHandler struct {
	store *store.Store
}

func NewGroupHandler(s *store.Store) *GroupHandler {
	return &GroupHandler{store: s}
}

type createGroupRequest struct {
	Name string `json:"name"`
}

func (h *GroupHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := requireUserUUID(w, r)
	if !ok {
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "group name is required")
		return
	}

	var created db.Group
	err := h.store.ExecTx(r.Context(), func(q *db.Queries) error {
		g, txErr := q.CreateGroup(r.Context(), db.CreateGroupParams{
			Name:      req.Name,
			CreatedBy: userUUID,
		})
		if txErr != nil {
			return txErr
		}

		if txErr := q.AddGroupMember(r.Context(), db.AddGroupMemberParams{
			GroupID: g.ID,
			UserID:  userUUID,
		}); txErr != nil {
			return txErr
		}

		created = g
		return nil
	})
	if err != nil {
		slog.Error("failed to create group", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *GroupHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	userUUID, ok := requireUserUUID(w, r)
	if !ok {
		return
	}

	groups, err := h.store.ListUserGroups(r.Context(), userUUID)
	if err != nil {
		slog.Error("failed to list groups", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, groups)
}

type addMemberRequest struct {
	Email string `json:"email"`
}

func (h *GroupHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	_, groupUUID, ok := authorizeGroupMember(w, r, h.store)
	if !ok {
		return
	}

	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	targetUser, err := h.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to lookup user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.store.AddGroupMember(r.Context(), db.AddGroupMemberParams{
		GroupID: groupUUID,
		UserID:  targetUser.ID,
	}); err != nil {
		slog.Error("failed to add group member", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "member added",
		"user_id": targetUser.ID,
		"name":    targetUser.Name,
	})
}
