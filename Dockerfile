# focodebase/fobackend/Dockerfile

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
RUN go build -o /sagrenti-install ./internal/server/cmd/install


# ============================================================
# Stage 2: Test
# ============================================================

FROM builder AS test

CMD ["go", "test", "./..."]


# ============================================================
# Stage 3: Production runtime
# ============================================================

FROM alpine:3.22 AS runtime

COPY --from=builder /main /main
COPY --from=builder /sagrenti-install /sagrenti-install

COPY keys /keys
COPY keys.pub /keys.pub

EXPOSE 8080

CMD ["/main"]