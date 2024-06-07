FROM alpine:3.17

RUN apk add --update curl ca-certificates && rm -rf /var/cache/apk* # Certificates for SSL

COPY targetgroup-sidecar .
ENTRYPOINT [ "./targetgroup-sidecar" ]
