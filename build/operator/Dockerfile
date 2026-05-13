# Build stage
FROM --platform=$BUILDPLATFORM registry.suse.com/bci/golang:1.26 AS builder

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
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o aif-operator ./cmd/operator

# Runtime stage
FROM registry.suse.com/bci/bci-micro:15.7

COPY --from=builder /workspace/aif-operator /aif-operator

USER 1000

ENTRYPOINT ["/aif-operator"]
