FROM --platform=$BUILDPLATFORM golang:1.22.5-alpine3.20 AS builder

ENV GOSUMDB=off

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY pkg/ pkg/

RUN go mod tidy

COPY --from=builder /workspace/pkg/ /usr/local/lib/

USER 1001
