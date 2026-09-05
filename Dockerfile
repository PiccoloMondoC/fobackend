# Future Offering backend Dockerfile

# Stage 1: Build
FROM golang:1.25 AS builder

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /main ./internal/server/cmd/api

# Stage 2: Runtime
FROM alpine:3.22

COPY --from=builder /main /main

EXPOSE 8080

CMD ["/main"]
