# --- build stage ---
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# static, stripped binary for a tiny image
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gostash .

# --- runtime stage ---
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