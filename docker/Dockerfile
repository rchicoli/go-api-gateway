FROM alpine:3.4

ENV APPLICATION_NAME go-api-gateway
ENV PATH /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/app

ENV MAX_WORKERS 500
ENV MAX_QUEUES 500
ENV MAX_LENGTH 1048576

RUN apk --update add tzdata \
    && cp /usr/share/zoneinfo/Europe/Berlin /etc/localtime \  
    && addgroup -S app \
    && adduser -s /sbin/nologin -D -S -h /app -G app app

COPY go-api-gateway /app/

EXPOSE 8080

USER app
WORKDIR /app

CMD ["go-api-gateway"]
