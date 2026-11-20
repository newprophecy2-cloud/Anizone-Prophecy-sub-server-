# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install certificates and build tools
RUN apk add --no-cache ca-certificates git

# Cache dependencies
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Final runtime image
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/server /app/server

EXPOSE 8000

ENV PORT=8000 \
    HOST=0.0.0.0

CMD ["/app/server"]
