# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agent-router ./cmd/router

# Run stage
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /agent-router .
COPY configs/config.example.yaml ./config.yaml
EXPOSE 8080
ENTRYPOINT ["./agent-router", "-config", "./config.yaml"]