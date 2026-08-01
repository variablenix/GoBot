# syntax=docker/dockerfile:1

FROM golang:1.25.12-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /irc-bot ./cmd/irc-bot
FROM alpine:latest
RUN addgroup -S gobot && adduser -S -G gobot gobot
WORKDIR /app
COPY --from=builder /irc-bot /irc-bot
COPY config.yaml /app/config.yaml
RUN chown -R gobot:gobot /app
USER gobot
EXPOSE 8082
ENTRYPOINT ["/irc-bot"]
