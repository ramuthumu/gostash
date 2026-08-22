# syntax=docker/dockerfile:1
# Multi-stage build: self-contained, no prebuilt binary required in context.
# VERSION is stamped into the binary via -X main.version.

FROM golang:1.25-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/gostash .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /data app
WORKDIR /data
USER app
COPY --from=build /out/gostash /usr/local/bin/gostash

ENV READLATER_ADDR=:8090
ENV READLATER_DATA=/data
EXPOSE 8090
VOLUME ["/data"]
ENTRYPOINT ["gostash"]
