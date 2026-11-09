# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# renovate: datasource=apk depName=vips versioning=apk
ARG VIPS_VERSION=8.18.2-r0
# renovate: datasource=apk depName=libjxl versioning=apk
ARG JXL_VERSION=0.11.2-r1
# renovate: datasource=apk depName=libwebp versioning=apk
ARG WEBP_VERSION=1.6.0-r0
# renovate: datasource=apk depName=lcms2 versioning=apk
ARG LCMS_VERSION=2.19-r0
# renovate: datasource=apk depName=libheif versioning=apk
ARG HEIF_VERSION=1.23.4-r0
# renovate: datasource=apk depName=imagemagick versioning=apk
ARG MAGICK_VERSION=7.1.2.35-r0

FROM golang:1.26.10-alpine3.24@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308 AS development
ARG VIPS_VERSION
ARG JXL_VERSION
ARG WEBP_VERSION
ARG LCMS_VERSION
ARG HEIF_VERSION
ARG MAGICK_VERSION
RUN apk add --no-cache build-base pkgconf \
    vips-dev=${VIPS_VERSION} vips-jxl=${VIPS_VERSION} \
    vips-heif=${VIPS_VERSION} vips-magick=${VIPS_VERSION} \
    libheif-dev=${HEIF_VERSION} imagemagick=${MAGICK_VERSION} \
    libjxl=${JXL_VERSION} libwebp=${WEBP_VERSION} lcms2=${LCMS_VERSION}
WORKDIR /app
ENV CGO_ENABLED=1
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM development AS check
RUN --mount=type=cache,target=/root/.cache/go-build make check-local

FROM development AS build
RUN --mount=type=cache,target=/root/.cache/go-build go build -trimpath -o /out/image-transform ./cmd/image-transform

FROM development AS smoke-build
RUN --mount=type=cache,target=/root/.cache/go-build go build -trimpath -o /out/smoke ./cmd/smoke

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS runtime-base
ARG VIPS_VERSION
ARG JXL_VERSION
ARG WEBP_VERSION
ARG LCMS_VERSION
ARG HEIF_VERSION
ARG MAGICK_VERSION
RUN apk add --no-cache ca-certificates \
    vips=${VIPS_VERSION} vips-jxl=${VIPS_VERSION} \
    vips-heif=${VIPS_VERSION} vips-magick=${VIPS_VERSION} \
    libheif=${HEIF_VERSION} imagemagick=${MAGICK_VERSION} \
    libjxl=${JXL_VERSION} libwebp=${WEBP_VERSION} lcms2=${LCMS_VERSION}
WORKDIR /app
COPY --from=build /out/image-transform /usr/local/bin/image-transform
COPY LICENSE NOTICE /usr/share/doc/image-transform/
LABEL org.opencontainers.image.title="Image Transform" \
      org.opencontainers.image.description="Inspect and convert still and animated images to WebP or JPEG XL" \
      org.opencontainers.image.source="https://github.com/kilianlabs/image-transform"
ENV HOST=0.0.0.0 PORT=3000 TMPDIR=/tmp
USER 1000:1000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s CMD ["image-transform", "healthcheck"]
ENTRYPOINT ["image-transform"]

FROM runtime-base AS smoke
COPY --from=smoke-build /out/smoke /usr/local/bin/smoke
COPY openapi.json /app/openapi.json
COPY internal/native/testdata /app/testdata
ENTRYPOINT ["smoke"]

FROM runtime-base AS runtime
