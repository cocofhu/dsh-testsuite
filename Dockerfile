# Control-plane binary. Needs a docker CLI to talk to the mounted daemon socket.
FROM golang:1.24-bookworm AS build

ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=dev
ENV GOPROXY=$GOPROXY CGO_ENABLED=0

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o /out/dsh-testsuite ./cmd/dsh-testsuite

FROM alpine:3.20

RUN apk add --no-cache ca-certificates docker-cli tzdata

COPY --from=build /out/dsh-testsuite /usr/local/bin/dsh-testsuite
COPY web /app/web
COPY config.example.yaml /app/config.yaml

ENV DSHTS_DATA=/data
EXPOSE 8090
WORKDIR /app

ENTRYPOINT ["/usr/local/bin/dsh-testsuite"]
CMD ["-config", "/app/config.yaml"]
