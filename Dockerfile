FROM golang:1.26-alpine AS builder

ARG VERSION=dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /envfuse ./cmd/envfuse/

FROM scratch

LABEL org.opencontainers.image.source="https://github.com/piotrek-b/envfuse"
LABEL org.opencontainers.image.description="Deterministic secrets injector and PID 1 supervisor for containers"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=builder /envfuse /envfuse

ENTRYPOINT ["/envfuse"]
