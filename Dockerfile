# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM oven/bun:1.4-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun x vite build --outDir /out --emptyOutDir

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /out ./web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X github.com/BioHazard786/Warpdrop/backend/internal/version.Version=${VERSION#v}" -o /warpdrop ./cmd/server

FROM gcr.io/distroless/static:nonroot
COPY --from=build /warpdrop /warpdrop
ENV PORT=8080
EXPOSE 8080
# distroless has no curl; the binary checks itself (flag added in the backend phase)
HEALTHCHECK --interval=15s --timeout=3s CMD ["/warpdrop", "-healthcheck"]
ENTRYPOINT ["/warpdrop"]
