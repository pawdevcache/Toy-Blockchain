# Multi-stage build: compile with the Go toolchain, ship only the binary.
# The result is a few megabytes and contains no shell, no package manager and
# no source, which is about as small an attack surface as a Go CLI can have.

FROM golang:1.23-alpine AS build
WORKDIR /src

# go.mod is copied first so this layer is cached until dependencies change.
# There is no go.sum: the project uses only the standard library.
COPY go.mod ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 produces a static binary that runs on an empty image.
# -trimpath keeps build paths out of the binary; -s -w drop the debug tables.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tbc ./cmd/tbc

# Verify the image is shipping a build that actually passes its tests.
RUN go vet ./... && go test ./...

# Prepare the data directory here, since the final image has no shell to mkdir.
RUN mkdir -p /data && chown 65532:65532 /data

FROM scratch AS runtime
COPY --from=build /out/tbc /tbc
COPY --from=build --chown=65532:65532 /data /data

# Run unprivileged: nothing here needs root.
USER 65532:65532
WORKDIR /data

# Persist the chain in the volume, not in the container's writable layer.
ENV TBC_DATA_FILE=/data/chain.json
VOLUME ["/data"]

ENTRYPOINT ["/tbc"]
CMD ["print"]
