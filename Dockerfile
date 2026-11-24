# Stage 1: Build the static Go binary
FROM golang:1.27.4-alpine AS builder

WORKDIR /app

# Cache dependencies first (only re-runs when go.mod/go.sum change)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and compile
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /splitflow-api ./cmd/api

# Stage 2: Minimal, secure runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /splitflow-api /splitflow-api

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/splitflow-api"]