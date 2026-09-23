# Future Offering backend Dockerfile

# ============================================================
# Stage 1: Build foundation
# ============================================================

FROM golang:1.25 AS builder

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /main ./internal/server/cmd/api


# ============================================================
# Stage 2: Test
# ============================================================
#
# Retains the Go toolchain and complete source tree so package,
# race, and PostgreSQL integration tests can run inside Docker.
#
# Example:
#
#   docker build --target test -t focodebase-go-backend-test ./fobackend
#
# This stage is not used by the production runtime image.

FROM builder AS test

CMD ["go", "test", "./..."]


# ============================================================
# Stage 3: Production runtime
# ============================================================

FROM alpine:3.22 AS runtime

COPY --from=builder /main /main

COPY keys /keys
COPY keys.pub /keys.pub

EXPOSE 8080

CMD ["/main"]
