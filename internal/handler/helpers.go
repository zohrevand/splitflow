package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/zohrevand/splitflow/internal/auth"
	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(s)
	return u, err
}

func authorizeGroupMember(w http.ResponseWriter, r *http.Request, st *store.Store) (userUUID, groupUUID pgtype.UUID, ok bool) {
	userUUID, ok = requireUserUUID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	groupUUID, err := parseUUID(chi.URLParam(r, "groupID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	isMember, err := st.IsGroupMember(r.Context(), db.IsGroupMemberParams{
		GroupID: groupUUID,
		UserID:  userUUID,
	})
	if err != nil || !isMember {
		writeError(w, http.StatusForbidden, "you are not a member of this group")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	return userUUID, groupUUID, true
}

func requireUserUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userIDStr, hasUser := auth.UserIDFromContext(r.Context())
	if !hasUser {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return pgtype.UUID{}, false
	}

	userUUID, err := parseUUID(userIDStr)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid user id")
		return pgtype.UUID{}, false
	}

	return userUUID, true
}
