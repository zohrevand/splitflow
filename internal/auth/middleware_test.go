package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zohrevand/splitflow/internal/auth"
)

func TestRequireAuth(t *testing.T) {
	svc := auth.NewService("test-secret-key")
	validToken, err := svc.GenerateToken("user-123")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Dummy handler that only runs if RequireAuth allows the request through
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok || userID != "user-123" {
			t.Errorf("expected userID 'user-123' in context, got %q (ok=%v)", userID, ok)
		}
		w.WriteHeader(http.StatusOK)
	})

	protected := svc.RequireAuth(nextHandler)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "missing header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong scheme",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer not.a.valid.jwt",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid token passes through",
			authHeader: "Bearer " + validToken,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
