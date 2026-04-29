FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ai-usage-dashboard ./cmd/server

FROM alpine:3.22

RUN adduser -D -H -u 10001 appuser
WORKDIR /app
COPY --from=build /out/ai-usage-dashboard /app/ai-usage-dashboard
COPY web/static /app/web/static
COPY config.example.json /app/config.example.json
RUN mkdir -p /data && chown -R appuser:appuser /data

USER appuser
ENV AI_USAGE_CONFIG=/app/config.json
ENV AI_USAGE_DB=/data/usage.db
ENV AI_USAGE_STATIC_DIR=/app/web/static
EXPOSE 8080
CMD ["/app/ai-usage-dashboard"]

