# Multi-stage Dockerfile for notify-klipper
# Stage 1: Build binary
FROM golang:alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-w -s" -o /notify-klipper ./cmd/notify-klipper

# Stage 2: Minimal runtime container
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /notify-klipper /app/notify-klipper

USER appuser

ENTRYPOINT ["/app/notify-klipper"]
