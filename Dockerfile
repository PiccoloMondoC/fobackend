# CGO remains disabled; image encoding uses the installed WebP utility.
# focodebase/fobackend/Dockerfile
FROM golang:1.25 AS builder
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /main ./internal/server/cmd/api
RUN go build -o /platform-install ./internal/server/cmd/install
RUN go build -o /platform-recovery ./internal/server/cmd/governance-recovery

FROM builder AS test
RUN apt-get update && apt-get install -y --no-install-recommends webp && rm -rf /var/lib/apt/lists/*
CMD ["go", "test", "./..."]

FROM alpine:3.22 AS runtime
RUN apk add --no-cache libwebp-tools
COPY --from=builder /main /main
COPY --from=builder /platform-install /platform-install
COPY --from=builder /platform-recovery /platform-recovery
COPY keys /keys
COPY keys.pub /keys.pub
EXPOSE 8080
CMD ["/main"]
