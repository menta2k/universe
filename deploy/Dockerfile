# Production image for netbootd: the Vue web UI and the database migrations
# are embedded into the single Go binary (internal/webui via go:embed and
# internal/data/migrations via go:embed).
#
# Build from the repository root:
#   docker build -f deploy/Dockerfile -t netbootd .
#
# The container needs a config file (see backend/configs/) mounted at
# /etc/netbootd/netbootd.yaml, and typically host networking plus
# NET_ADMIN/NET_RAW capabilities for the DHCP/TFTP services.

FROM node:22-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=webui /src/frontend/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/netbootd ./cmd/netbootd

# netbootd binds privileged ports (DHCP 67, TFTP 69) — run as root inside the
# container and constrain it with capabilities at the compose/orchestrator level.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/netbootd /usr/local/bin/netbootd
ENTRYPOINT ["/usr/local/bin/netbootd"]
CMD ["-conf", "/etc/netbootd/netbootd.yaml"]
