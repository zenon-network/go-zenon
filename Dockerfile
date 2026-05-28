# Build Stage
FROM golang:1.20-alpine AS builder

# Install build dependencies
RUN apk add --no-cache make gcc musl-dev git

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build znnd
RUN make znnd

# Final Stage
FROM alpine:latest

# Install runtime dependencies (if any, like ca-certificates)
RUN apk add --no-cache ca-certificates

# Set working directory
WORKDIR /root

# Create the data directory
RUN mkdir -p /root/.znn

# Copy the binary from the builder stage
COPY --from=builder /app/build/znnd /usr/local/bin/znnd

# Bake committed devnet assets (genesis, per-role configs, pillar keystore + p2p key).
COPY docker/devnet /devnet

# Expose ports:
# 35995: P2P
# 35997: HTTP RPC
# 35998: WS RPC
EXPOSE 35995 35997 35998

# Role-aware entrypoint (picks pillar|rpc via $ZNND_ROLE).
COPY docker/devnet/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
