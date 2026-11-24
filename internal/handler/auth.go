package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/zohrevand/splitflow/internal/auth"
	"github.com/zohrevand/splitflow/internal/store"
	"github.com/zohrevand/splitflow/internal/store/db"
)

type AuthHandler struct {
	store   *store.Store
	authSvc *auth.Service
}

func NewAuthHandler(s *store.Store, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{store: s, authSvc: authSvc}
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenPair struct {
	accessToken string
	rawRefresh  string
	hashRefresh string
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	if req.Email == "" || req.Name == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email, name, and password (min 8 chars) are required")
		return
	}

	hash, err := h.authSvc.HashPassword(req.Password)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	user, err := h.store.CreateUser(r.Context(), db.CreateUserParams{
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		slog.Error("failed to create user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		slog.Error("failed to query user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if !h.authSvc.CheckPassword(req.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	tokens, err := h.generateTokenPair(user.ID.String())
	if err != nil {
		slog.Error("failed to generate tokens", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.store.CreateRefreshToken(r.Context(), newRefreshTokenParams(user.ID, tokens.hashRefresh)); err != nil {
		slog.Error("failed to store refresh token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.accessToken,
		"refresh_token": tokens.rawRefresh,
		"expires_in":    int(auth.AccessTokenTTL.Seconds()),
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
		},
	})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := h.decodeRefreshTokenHash(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	stored, err := h.store.GetRefreshTokenByHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		slog.Error("failed to lookup refresh token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if stored.RevokedAt.Valid || time.Now().After(stored.ExpiresAt.Time) {
		writeError(w, http.StatusUnauthorized, "refresh token expired or revoked")
		return
	}

	tokens, err := h.generateTokenPair(stored.UserID.String())
	if err != nil {
		slog.Error("failed to generate tokens", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	err = h.store.ExecTx(r.Context(), func(q *db.Queries) error {
		if txErr := q.RevokeRefreshToken(r.Context(), stored.ID); txErr != nil {
			return txErr
		}
		return q.CreateRefreshToken(r.Context(), newRefreshTokenParams(stored.UserID, tokens.hashRefresh))
	})
	if err != nil {
		slog.Error("failed to rotate refresh token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.accessToken,
		"refresh_token": tokens.rawRefresh,
		"expires_in":    int(auth.AccessTokenTTL.Seconds()),
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := h.decodeRefreshTokenHash(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	if err := h.store.RevokeRefreshTokenByHash(r.Context(), tokenHash); err != nil {
		slog.Error("failed to revoke refresh token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) generateTokenPair(userID string) (tokenPair, error) {
	accessToken, err := h.authSvc.GenerateToken(userID)
	if err != nil {
		return tokenPair{}, fmt.Errorf("generate access token: %w", err)
	}

	rawRefresh, hashRefresh, err := h.authSvc.GenerateRefreshToken()
	if err != nil {
		return tokenPair{}, fmt.Errorf("generate refresh token: %w", err)
	}

	return tokenPair{
		accessToken: accessToken,
		rawRefresh:  rawRefresh,
		hashRefresh: hashRefresh,
	}, nil
}

func (h *AuthHandler) decodeRefreshTokenHash(r *http.Request) (string, bool) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", false
	}
	raw := strings.TrimSpace(req.RefreshToken)
	if raw == "" {
		return "", false
	}
	return h.authSvc.HashRefreshToken(raw), true
}

func newRefreshTokenParams(userID pgtype.UUID, tokenHash string) db.CreateRefreshTokenParams {
	return db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(auth.RefreshTokenTTL), Valid: true},
	}
}
