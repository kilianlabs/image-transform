# Contributing

To use Image Transform in your application, start with the
[Docker guide](README.md#run-with-docker). This guide covers code changes and tests.

## Local setup

Install Docker and Make, then clone the repository and build the image:

```sh
git clone https://github.com/kilianlabs/image-transform.git
cd image-transform
make build
docker run --rm -p 127.0.0.1:3000:3000 --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=2147483648 \
  --memory=2g --cpus=2 --pids-limit=128 image-transform:local
```

The service listens on `http://127.0.0.1:3000`. Docker supplies Go, the C compiler,
and libvips with its input decoders and WebP/JPEG XL support. Dependency downloads
need network access; image processing and tests run locally with synthetic images.

For host development, install the Go version from [go.mod](go.mod), a C compiler,
`pkg-config`, libheif development headers, and libvips development libraries with
WebP/JPEG XL, HEIF, TIFF, and Magick support. HEIF decoding needs HEVC and AV1
plugins; BMP needs the ImageMagick BMP coder. Image processing calls these
libraries through libvips, without running codec commands. Use `CGO_ENABLED=1`
and `make check-local`.

## Checks

```sh
make check
```

This runs formatting checks, `go vet`, race-enabled tests, a build, OpenAPI
validation and drift checks, a generated-client test, and a container smoke test.
Useful commands while developing:

```sh
make format   # format Go sources and the client-test template
make openapi  # regenerate the API contract
make build    # build image-transform:local
make smoke    # test the runtime container
```

`make openapi` needs Go but no native libraries. Commit `openapi.json` with API
changes and `go.sum` with dependency changes. The client check uses the versions
pinned in [scripts/check-client.sh](scripts/check-client.sh).

[Renovate](.github/renovate.json) checks dependencies on Monday mornings in
`Europe/Berlin`. Go patch/minor updates and Docker digest updates can auto-merge
after checks pass; other updates require review. Native package pins are grouped
and shared by the build and runtime. Update both stages and the Renovate registry
setting together when moving to a new Alpine release.

Use [private vulnerability reporting](SECURITY.md) for security issues.

## Container testing

```sh
make smoke
```

The smoke test checks the actual Alpine server, including non-root execution,
BMP, TIFF, HEIC/HEIF, and AVIF inputs, still and animated WebP/JPEG XL conversion,
response headers, OpenAPI, and shutdown with an incomplete request. It runs with
a read-only filesystem and resource limits.

CI tests Linux AMD64 and ARM64. When changing native libraries, base images, or
processing options, run both platforms locally with Docker's emulation support:

```sh
make smoke-platforms
```

To test one platform, use `make smoke PLATFORM=linux/amd64` or
`make smoke PLATFORM=linux/arm64`. CI uses the same Make targets for the project
checks and container tests.

Compare visual quality on representative images when changing codec defaults.
For available image tags and deployment updates, see [image tags and upgrades](docs/releases.md).

## Preparing a release

1. Set `Version` in [internal/buildinfo/version.go](internal/buildinfo/version.go)
   to the release version, then run `make openapi`. Include the generated contract
   with the version change.
2. Run `make check` and `make smoke-platforms`. The latter tests the runtime on
   both supported architectures. Review changes to the API, configuration, and
   native libraries and update the relevant documentation.
3. After the changes land on `main`, create a tag named `v<version>`, such as
   `v0.1.0`, on the release commit. Pushing the tag triggers the container workflow.

Release CI requires the tag, source version, and OpenAPI version to match. The
container workflow runs all checks before publishing AMD64 and ARM64 images to
GHCR. See [NOTICE](NOTICE) for the packaged native libraries' distribution terms.
