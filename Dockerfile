FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" -o /out/CLIProxyAPI ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/cli-console ./cmd/console/

FROM alpine:3.23 AS api

RUN apk add --no-cache tzdata

RUN mkdir /CLIProxyAPI

COPY --from=builder /out/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI

COPY config.example.yaml /CLIProxyAPI/config.example.yaml

WORKDIR /CLIProxyAPI

EXPOSE 8317

ENV TZ=Asia/Shanghai

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

CMD ["./CLIProxyAPI"]

FROM alpine:3.23 AS console

RUN apk add --no-cache tzdata

RUN mkdir /CLIProxyAPI

COPY --from=builder /out/cli-console /CLIProxyAPI/cli-console

COPY config.example.yaml /CLIProxyAPI/config.example.yaml

WORKDIR /CLIProxyAPI

EXPOSE 8318

ENV TZ=Asia/Shanghai

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

CMD ["./cli-console"]

FROM nginx:1.29-alpine AS console-proxy

RUN apk add --no-cache apache2-utils gettext

COPY docker/nginx/console-proxy.conf.template /etc/nginx/templates/console-proxy.conf.template
COPY docker/nginx/start-console-proxy.sh /usr/local/bin/start-console-proxy.sh

RUN chmod +x /usr/local/bin/start-console-proxy.sh

CMD ["/usr/local/bin/start-console-proxy.sh"]
