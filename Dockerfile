# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/
COPY tools.go ./

# Build the operator binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -o aif-operator ./cmd/operator

# Runtime stage
FROM alpine:3.18

# Install ca-certificates for HTTPS calls
RUN apk --no-cache add ca-certificates

# Create non-root user with UID 1000
RUN adduser -D -u 1000 -g "" aif-operator

WORKDIR /

# Copy the binary from builder
COPY --from=builder /workspace/aif-operator .

# Set user to UID 1000
USER 1000

ENTRYPOINT ["/aif-operator"]
