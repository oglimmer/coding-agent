# Multi-stage: build a static Go binary, ship it on distroless nonroot.
FROM golang:1.26-alpine AS build
WORKDIR /src

ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_TIME=unknown

# Cache modules before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/oglimmer/coding-agent/backend/internal/buildinfo.Version=${VERSION} \
      -X github.com/oglimmer/coding-agent/backend/internal/buildinfo.Commit=${GIT_COMMIT} \
      -X github.com/oglimmer/coding-agent/backend/internal/buildinfo.Time=${BUILD_TIME}" \
    -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
