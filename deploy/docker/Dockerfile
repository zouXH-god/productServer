FROM node:20-alpine AS frontend-builder

WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm --prefix frontend ci
COPY frontend ./frontend
RUN mkdir -p /src/internal/server/frontend/dist \
    && npm --prefix frontend run build

FROM golang:1.23-alpine AS backend-builder

WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/internal/server/frontend/dist ./internal/server/frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/productserver .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S productserver \
    && adduser -S -G productserver -h /app productserver \
    && mkdir -p /app/data/artifacts /app/data/logs /app/data/work \
    && chown -R productserver:productserver /app

WORKDIR /app
COPY --from=backend-builder /out/productserver /app/productserver

USER productserver
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/productserver"]
CMD ["server"]
