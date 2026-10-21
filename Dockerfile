FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# migrations and the op name files are embedded into the binary
COPY main.go jettons.go op_names.tsv op_names_registry.tsv ./
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ton-blocks .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /ton-blocks /ton-blocks
ENV TON_HEARTBEAT_FILE=/tmp/ton-blocks.heartbeat

# unhealthy when no masterchain block was processed for 3 minutes
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s --retries=3 \
  CMD ["/ton-blocks", "-healthcheck"]

ENTRYPOINT ["/ton-blocks"]
