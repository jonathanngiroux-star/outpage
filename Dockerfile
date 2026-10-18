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
# Bind to loopback behind your reverse proxy (docs/custom-domain.md).
VOLUME ["/data"]
ENV OUTPAGE_DB=/data/outpage.db OUTPAGE_HTTP=127.0.0.1:8080
EXPOSE 8080
ENTRYPOINT ["/outpage"]
CMD ["serve"]
