FROM --platform=$BUILDPLATFORM registry.suse.com/bci/golang:1.26 AS builder

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o aif-operator ./cmd/operator

FROM registry.suse.com/bci/bci-micro:15.7

COPY --from=builder /workspace/aif-operator /aif-operator

USER 1000

ENTRYPOINT ["/aif-operator"]
