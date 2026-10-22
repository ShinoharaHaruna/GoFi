# syntax=docker/dockerfile:1
FROM golang:1.26.10-bookworm AS build

WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=2 GOMEMLIMIT=512MiB

COPY go.mod go.sum ./
RUN go mod download

# Keep runtime configuration and uploads outside every build stage.
COPY VERSION ./
COPY cmd/gofi/*.go ./cmd/gofi/
COPY cmd/docs/docs.go ./cmd/docs/
COPY internal/config/*.go ./internal/config/
COPY internal/database/*.go ./internal/database/
COPY internal/handlers/*.go ./internal/handlers/
COPY internal/models/*.go ./internal/models/
COPY internal/router/*.go ./internal/router/
COPY internal/utility/*.go ./internal/utility/

ARG VCS_REF=unknown
RUN version="$(tr -d '\r\n' < VERSION)" && test -n "$version" \
    && go build -p 1 -mod=readonly -buildvcs=false -trimpath \
       -ldflags="-s -w -X main.version=${version}-${VCS_REF}" \
       -o /out/GoFi ./cmd/gofi

FROM debian:bookworm-slim AS runtime

RUN apt-get update -o APT::Update::Error-Mode=any \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 gofi \
    && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /app --shell /usr/sbin/nologin gofi \
    && install -d -o 10001 -g 10001 -m 0750 /app /app/data /app/data/public /app/data/private

ARG VCS_REF=unknown
LABEL org.opencontainers.image.revision="${VCS_REF}"

WORKDIR /app
COPY --from=build /out/GoFi /app/GoFi
USER 10001:10001
EXPOSE 8080

# This checks HTTP liveness, not database or storage readiness.
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD ["curl", "--fail", "--silent", "--show-error", "--max-time", "3", "--output", "/dev/null", "http://127.0.0.1:8080/health"]

ENTRYPOINT ["/app/GoFi", "-config", "/app/config.toml"]
