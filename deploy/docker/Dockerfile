# tgp-server image. Build from the repository root:
#   docker build -f deploy/docker/Dockerfile -t tgp-server .
# Data lives in /var/lib/tgp (a volume); configure with TGP_* variables or
# mount a config file and pass `-c`.

FROM node:24-bookworm-slim AS web
WORKDIR /src/web
RUN npm install -g pnpm@12
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27-bookworm AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X git.taile67276.ts.net/BloxBlast/tgp/internal/version.Version=${VERSION} -X git.taile67276.ts.net/BloxBlast/tgp/internal/version.Commit=${COMMIT}" \
      -o /out/tgp-server ./cmd/tgp-server \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/tgp ./cmd/tgp

FROM debian:bookworm-slim
# git serves and stores repositories; curl is for the health check.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates curl tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 1000 --home-dir /var/lib/tgp --shell /usr/sbin/nologin tgp \
 && install -d -m 0750 -o tgp -g tgp /var/lib/tgp
COPY --from=go /out/tgp-server /out/tgp /usr/local/bin/
USER 1000:1000
ENV TGP_DATA_DIR=/var/lib/tgp \
    TGP_LISTEN=:3000
VOLUME /var/lib/tgp
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["curl", "-fsS", "-o", "/dev/null", "http://127.0.0.1:3000/readyz"]
ENTRYPOINT ["tgp-server"]
CMD ["serve"]
