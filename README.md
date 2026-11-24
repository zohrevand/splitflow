# SplitFlow API

A production-oriented collaborative expense and bill-splitting backend built in **Go** and **PostgreSQL**, designed to serve mobile clients reliably over unstable networks.

## Key Engineering Highlights

- **Idiomatic, Framework-Free Go:** Built on `net/http` + `go-chi/chi` with explicit HTTP server timeouts, graceful shutdown (`SIGTERM`/`SIGINT`), and structured JSON logging (`log/slog`).
- **Type-Safe SQL without ORM Magic:** Uses `pgx/v5` + `sqlc` to generate compile-time safe Go code from raw PostgreSQL queries, paired with versioned `goose` migrations.
- **Financial Integrity:** All monetary values are stored as `BIGINT` cents (zero floating-point math). Even splits use deterministic remainder distribution so pennies never vanish.
- **Ledger-Based Balances & Debt Simplification:** Balances are computed dynamically from an immutable ledger (`expenses`, `expense_splits`, `settlements`), and a greedy min-cash-flow algorithm calculates the minimal transfers needed to settle a group.
- **Mobile-First Reliability:**
  - **Idempotency Keys:** `POST /expenses` supports an `Idempotency-Key` header backed by a PostgreSQL unique index to prevent duplicate charges on mobile network retries.
  - **Cursor Pagination:** Keyset pagination (`created_at, id`) avoids skipped/duplicate rows during infinite scroll.
  - **Rotating Refresh Tokens:** Short-lived JWT access tokens (15m) paired with SHA-256 hashed, single-use rotating refresh tokens (30d) stored in PostgreSQL.
- **Security & Observability:** Per-IP token-bucket rate limiting on auth endpoints (`golang.org/x/time/rate`) with background memory cleanup, plus structured request logging.
- **Real Database Testing:** Table-driven unit tests for domain logic, `httptest` for auth middleware, and **`testcontainers-go`** integration tests running against an ephemeral PostgreSQL 16 container.

## Tech Stack

| Layer | Technology |
| :--- | :--- |
| Language | Go |
| Router & HTTP | `net/http`, `go-chi/chi/v5` |
| Database & Driver | PostgreSQL 16, `pgx/v5` (`pgxpool`) |
| SQL Codegen & Migrations | `sqlc`, `goose/v3` |
| Auth | `golang-jwt/jwt/v5`, `bcrypt`, SHA-256 opaque refresh tokens |
| API Docs | Embedded OpenAPI 3.0 (`//go:embed`), Scalar UI |
| Testing | `testing`, `httptest`, `testcontainers-go` |
| Containerization & CI | Multi-stage `distroless` Dockerfile, Docker Compose, GitHub Actions |

## Quick Start

### 1. Run with Docker Compose (API + PostgreSQL)
```bash
docker compose up --build -d
```

### 2. Apply Migrations
```bash
goose -dir migrations postgres "postgres://postgres:secretpassword@localhost:5432/splitflow?sslmode=disable" up
```

### 3. Run Tests
```bash
# Fast unit tests only
go test -short -v ./...

# Full suite (including PostgreSQL integration tests via testcontainers-go)
go test -race -v ./...
```

## API Documentation

SplitFlow ships with an embedded **OpenAPI 3.0** specification (`//go:embed`) and an interactive **Scalar** UI—no external Swagger setup required.

Once the server is running (`docker compose up -d`), open:

- **Interactive API Docs (Scalar UI):** [`http://localhost:8080/docs`](http://localhost:8080/docs)
- **Raw OpenAPI 3.0 Spec (YAML):** [`http://localhost:8080/openapi.yaml`](http://localhost:8080/openapi.yaml)
- **Source File:** [`api/openapi.yaml`](api/openapi.yaml)

### Testing Protected Endpoints in the UI
1. Run `POST /auth/register` and `POST /auth/login` directly from `/docs`.
2. Copy the `access_token` from the login response.
3. Click **Authentication** (or the lock icon) in Scalar, select **BearerAuth**, and paste the token.