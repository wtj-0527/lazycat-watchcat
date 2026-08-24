# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS frontend
WORKDIR /src

COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm --prefix frontend ci --include=dev --no-audit --no-fund

COPY frontend ./frontend
RUN npm --prefix frontend run build

FROM golang:1.25-alpine AS backend
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath -ldflags="-s -w" -o /out/maoyan ./cmd/maoyan-hub

FROM alpine:3.22

RUN apk add --no-cache \
      btrfs-progs \
      ca-certificates \
      smartmontools \
      tzdata \
    && addgroup -S maoyan \
    && adduser -S -D -H -G maoyan maoyan \
    && mkdir -p /opt/maoyan/web

COPY --from=backend /out/maoyan /usr/local/bin/maoyan
COPY --from=frontend /src/web /opt/maoyan/web

ENV MAOYAN_ADDR=:8080 \
    MAOYAN_COLLECTOR_ADDR=:8443 \
    MAOYAN_DATA_DIR=/lzcapp/var/data \
    MAOYAN_WEB_DIR=/opt/maoyan/web

EXPOSE 8080 8443

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -q -T 3 -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/maoyan"]
