FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# migrations and the op name files are embedded into the binary
COPY *.go op_names.tsv op_names_registry.tsv ./
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ton-blocks .

# fift and its lib/ (Disasm.fif) from a TON release, for ton.code.asm. The
# release fift is an AppImage; there is no FUSE in a container, so it is
# extracted and run directly.
FROM debian:12-slim AS fift
ARG TON_RELEASE=v2026.10
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/fift
RUN curl -fsSL -o fift https://github.com/ton-blockchain/ton/releases/download/${TON_RELEASE}/fift-linux-x86_64 \
 && chmod +x fift && ./fift --appimage-extract >/dev/null && mv squashfs-root app && rm fift \
 && curl -fsSL -o lib.zip https://github.com/ton-blockchain/ton/releases/download/${TON_RELEASE}/smartcont_lib.zip \
 && unzip -q lib.zip 'lib/*' && rm lib.zip

# debian-slim rather than distroless: fift needs glibc and libtinfo
FROM debian:12-slim
COPY --from=fift /opt/fift /opt/fift
# fail the build here, not at runtime, if fift can't start
RUN LD_LIBRARY_PATH=/opt/fift/app/usr/lib /opt/fift/app/usr/bin/fift -V
COPY --from=build /ton-blocks /ton-blocks
ENV TON_HEARTBEAT_FILE=/tmp/ton-blocks.heartbeat FIFT_DIR=/opt/fift
USER 65532:65532

# unhealthy when no masterchain block was processed for 3 minutes
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 \
  CMD ["/ton-blocks", "-healthcheck"]

ENTRYPOINT ["/ton-blocks"]
