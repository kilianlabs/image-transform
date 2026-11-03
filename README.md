# Image Transform

Inspect images and convert them to WebP or JPEG XL through a small HTTP API.
Create thumbnails, fit images within a bounding box, or convert at full
resolution. Aspect ratio and transparency are preserved, without cropping or
upscaling.

Image Transform accepts JPEG, PNG, GIF, WebP, JPEG XL, BMP, TIFF, HEIC/HEIF, and
AVIF images, including animated GIF, WebP, and JPEG XL. It applies orientation,
converts colors to sRGB, and removes private metadata from the output. Processing
runs locally with libvips. Images are not sent to external services, and no
database or persistent storage is needed.

## Run with Docker

Container images use `ghcr.io/kilianlabs/image-transform` and support Linux AMD64
and ARM64. Use `edge` to try the current development version:

```sh
docker run --rm -p 127.0.0.1:3000:3000 --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=2147483648 \
  --memory=2g --cpus=2 --pids-limit=128 \
  ghcr.io/kilianlabs/image-transform:edge
```

For production, choose a versioned release or pin an image digest. There is no
`latest` tag. See [image tags and upgrades](docs/releases.md).

To add the service to an existing Docker Compose stack:

```yaml
services:
  image-transform:
    image: ghcr.io/kilianlabs/image-transform:edge
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=2147483648
    mem_limit: 2g
    cpus: 2
    pids_limit: 128
    stop_grace_period: 10s
```

Other services on the same network can call `http://image-transform:3000`.
No host port is needed for internal use. The API has no authentication; keep it
on a trusted private network. To build from source, see
[Contributing](CONTRIBUTING.md#local-setup).

## Inspect and convert an image

Send the image bytes as `application/octet-stream`. The service detects the
format from the image itself:

```sh
curl --fail-with-body http://localhost:3000/api/inspect \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.jpg
```

The JSON response contains the MIME type, file size, SHA-256 checksum, and
dimensions before and after applying orientation. Inspection also checks that
the image can be decoded.

To create a WebP image that fits within 750 × 750 pixels:

```sh
curl --fail-with-body 'http://localhost:3000/api/transform?format=webp&width=750&height=750' \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.jpg -o photo.webp
```

A 1200 × 800 image becomes 750 × 500. Smaller images keep their size. Omit
`width` and `height` to convert at full resolution, use `format=jxl` for JPEG XL,
or add `lossless=true` for lossless encoding. Each request returns one image;
your original file is unchanged.

| Endpoint | Purpose |
| --- | --- |
| `POST /api/inspect` | Validate an image and read its metadata |
| `POST /api/transform` | Convert or resize an image to WebP or JPEG XL |
| `GET /api/openapi.json` | Download the OpenAPI contract |
| `GET /health` | Check whether the service is ready |

Animations use the same conversion options as still images. Frame order,
timing, and loop behavior are retained, subject to the
[animation handling](docs/api.md#animations) described in the API guide.
Multi-page TIFFs use the first page; HEIF/AVIF collections use the primary image.
Detected APNG, animated HEIF/AVIF, SVG, and other unsupported input formats are
rejected. Decoders may recover damaged inputs or omit unrecognized animation
frames; see [input handling](docs/api.md).
Color conversion targets sRGB. HDR tone mapping and HEIF/AVIF NCLX-only color
profiles are not supported reliably; see
[color limitations](docs/api.md#color-and-hdr-limitations).

See the [API guide](docs/api.md) for options, response headers, and errors, or use
[openapi.json](openapi.json) to generate a client.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOST` | `0.0.0.0` in Docker; `127.0.0.1` locally | Listen address |
| `PORT` | `3000` | HTTP port |
| `TMPDIR` | `/tmp` in Docker; OS temporary directory locally | Writable temporary storage |
| `MAX_INPUT_BYTES` | `104857600` | 100 MiB input limit |
| `MAX_PIXELS` | `50000000` | 50 megapixels across all source frames |
| `MAX_FRAMES` | `500` | Maximum frames per input |
| `MAX_CONCURRENT` | `1` | Simultaneous image requests |
| `REQUEST_TIMEOUT` | `120s` | Upload and processing deadline |

The container runs as a non-root user and includes a health check. The examples
above provide a read-only root filesystem, temporary storage, and resource
limits. See [deployment and configuration](docs/operations.md) for all settings,
resource sizing, and troubleshooting.

## Help and feedback

Report bugs or suggest improvements in
[GitHub Issues](https://github.com/kilianlabs/image-transform/issues).
For security issues, see [SECURITY.md](SECURITY.md).
To contribute code, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Source code and documentation use the [MIT License](LICENSE).
The container includes native libraries with additional license requirements,
including GPL terms for distribution of the packaged executable.
See [NOTICE](NOTICE) for details.
