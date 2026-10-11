# focodebase/fobackend/Dockerfile
FROM golang:1.25 AS builder
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /main ./internal/server/cmd/api
RUN go build -o /sagrenti-install ./internal/server/cmd/install
RUN go build -o /sagrenti-recovery ./internal/server/cmd/governance-recovery
FROM builder AS test
CMD ["go", "test", "./..."]
FROM alpine:3.22 AS runtime
COPY --from=builder /main /main
COPY --from=builder /sagrenti-install /sagrenti-install
COPY --from=builder /sagrenti-recovery /sagrenti-recovery
COPY keys /keys
COPY keys.pub /keys.pub
EXPOSE 8080
CMD ["/main"]
