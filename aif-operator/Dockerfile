FROM --platform=$BUILDPLATFORM registry.suse.com/bci/golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=unknown
ARG COMMIT=unknown

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -a -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
