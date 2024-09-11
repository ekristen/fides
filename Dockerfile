# syntax=docker/dockerfile:1.10-labs

FROM debian:bookworm-slim as base
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -r -u 999 -d /home/fides fides

FROM ghcr.io/acorn-io/images-mirror/golang:1.21 AS build
COPY / /src
WORKDIR /src
RUN \
  --mount=type=cache,target=/go/pkg \
  --mount=type=cache,target=/root/.cache/go-build \
  go build -o bin/fides main.go

FROM base AS goreleaser
ENTRYPOINT ["/usr/local/bin/fides"]
CMD ["controller"]
COPY fides /usr/local/bin/fides
USER fides

FROM base
ENTRYPOINT ["/usr/local/bin/fides"]
CMD ["controller"]
COPY --from=build /src/bin/fides /usr/local/bin/fides
USER fides