FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -h /data app
WORKDIR /data
USER app
COPY gostash /usr/local/bin/gostash

ENV READLATER_ADDR=:8090
ENV READLATER_DATA=/data
EXPOSE 8090
VOLUME ["/data"]
ENTRYPOINT ["gostash"]