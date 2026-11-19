# aide-relay: self-hosted, untrusted coordinator (README: Run your own server).
#   docker run -d --name aide-relay --restart unless-stopped -p 80:80 -p 443:443 \
#     -v aide-relay:/data -e AIDE_RELAY_DOMAIN=relay.example.com -e AIDE_RELAY_MCP=1 \
#     ghcr.io/samir2015dzh/aide-relay
# Without AIDE_RELAY_DOMAIN it serves plain HTTP on :8080, for a proxy
# that serves HTTPS (then set AIDE_RELAY_PUBLIC_URL). Each AIDE_ variable
# is read as HERMESH_ too, as containers from before the rename set it.
# Built on the build platform for each target (linux/amd64, linux/arm64).
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# hermesh-relay beside it, the name it had (docker exec <c> hermesh-relay claim-code): RENAME-AIDE.md B4.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/bin/aide-relay ./cmd/hermesh-relay \
  && ln -s aide-relay /out/bin/hermesh-relay && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bin/ /usr/local/bin/
# The relay runs as nonroot: a new volume takes this directory's owner.
COPY --from=build --chown=65532:65532 /out/data /data
# docker exec aide-relay aide-relay claim-code (or mcp-url) finds it too.
ENV HERMESH_RELAY_DATA=/data
VOLUME /data
EXPOSE 80 443 8080
ENTRYPOINT ["/usr/local/bin/aide-relay"]
CMD ["serve"]
