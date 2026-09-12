# Build from the repository root:
#   docker build -f cmd/ptero-mcp/Dockerfile -t ptero-mcp .
#
# Only cmd/ptero-mcp and the two packages it uses are compiled, so the Wails
# desktop app's GUI toolchain is never needed here.
FROM golang:1.23-alpine AS build

WORKDIR /src

# Dependencies first, so a code change does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY pkg/ ./pkg/
COPY cmd/ptero-mcp/ ./cmd/ptero-mcp/

# CGO off and the symbol table stripped: a static binary that runs on scratch.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ptero-mcp ./cmd/ptero-mcp

FROM alpine:3.20

# The panel is reached over HTTPS, so the image needs root certificates. wget
# comes with busybox and serves the health check.
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 ptero

COPY --from=build /ptero-mcp /usr/local/bin/ptero-mcp

USER 10001
EXPOSE 8472

# A token is required for a non-loopback listener, so PTERO_MCP_TOKEN has to be
# supplied at run time; the server refuses to start without one rather than
# leaving the port open.
ENV PTERO_MCP_ADDR=:8472

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8472/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/ptero-mcp"]
