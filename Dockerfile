FROM alpine:3.8

RUN apk add --update curl ca-certificates && rm -rf /var/cache/apk* # Certificates for SSL

COPY targetgroup-sidecar .
ENTRYPOINT [ "./targetgroup-sidecar" ]
