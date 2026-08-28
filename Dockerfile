# Build on the native platform and cross-compile — pure Go, so this is fast.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first, so edits to the source do not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/buyforbub .

# Distroless has no shell to mkdir with, so the data directory is staged here.
# Without it Docker creates /data root-owned and the nonroot user cannot write.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/buyforbub /buyforbub
COPY --from=build --chown=65532:65532 /out/data /data

ENV PORT=8080 \
    DB_PATH=/data/buyforbub.db

EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot

# No shell or curl in distroless, so the binary checks itself.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/buyforbub", "-healthcheck"]

ENTRYPOINT ["/buyforbub"]
