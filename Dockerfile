# Self-improving MCP: proxy (hot path) and evolve (offline loop) in one image.
#
#   docker build -t simcp .
#   docker run -i --rm -v simcp-data:/data -v ./config:/config:ro simcp proxy -config /config/proxy.yaml
#   docker run --rm -v simcp-data:/data -v ./config:/config:ro --env-file .env simcp evolve versions -config /config/evolve.yaml
#
# Node is in the runtime image because the example upstream (an npx-launched MCP server) needs it.
# If your upstream is an HTTP url or a different runtime, swap the base image.

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/proxy ./cmd/proxy \
 && go build -trimpath -ldflags="-s -w" -o /out/evolve ./cmd/evolve

FROM node:22-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 simcp \
 && mkdir /data && chown simcp /data
COPY --from=build /out/proxy /out/evolve /usr/local/bin/
COPY docker/entrypoint.sh /usr/local/bin/simcp
RUN chmod +x /usr/local/bin/simcp
USER simcp
ENV DATA_DIR=/data
VOLUME /data
# Status endpoints (/health, /overlay) when proxy.yaml sets status_addr: 0.0.0.0:8787
EXPOSE 8787
ENTRYPOINT ["simcp"]
CMD ["proxy"]
