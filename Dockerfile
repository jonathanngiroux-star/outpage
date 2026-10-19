# Outpage — one static binary, one SQLite file, no runtime deps.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /outpage ./cmd/outpage

FROM scratch
COPY --from=build /outpage /outpage
# SQLite file lives on a mounted volume; scratch has no shell or TLS roots.
VOLUME ["/data"]
# Bind all interfaces INSIDE the container so the documented port mapping
# (-p 8080:8080) actually reaches the page: container-loopback is not
# reachable from the host. Exposure is governed by the -p mapping and
# your reverse proxy (docs/custom-domain.md), not by this default.
ENV OUTPAGE_DB=/data/outpage.db OUTPAGE_HTTP=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/outpage"]
CMD ["serve"]
