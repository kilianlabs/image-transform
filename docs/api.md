# Image API

Send one image per request as `application/octet-stream`. Supported inputs are
JPEG, PNG, GIF, WebP, JPEG XL, BMP, TIFF, HEIC/HEIF, and AVIF. GIF, WebP, and JPEG
XL can be animated. HEIC/HEIF supports HEVC-compressed images; AVIF uses AV1.
The service detects the format from the bytes; filenames and extensions do not
affect processing. Detected APNG, animated HEIF/AVIF image sequences, SVG, and
other unsupported formats are rejected.

Format detection reads a bounded header, and validity follows the native
decoders. Damaged or truncated files may be accepted with fewer frames;
unrecognized animations may be decoded as a still image. The service does not
guarantee file completeness or detection of every animation. Metadata describes
what the decoder loaded.

TIFF conversion uses the first page. HEIF/AVIF image collections use the primary
image identified by the file. Other pages, thumbnails, and auxiliary images are
not included in the output. Inspection reports the selected image and its
position in the collection.

By default, an input can be up to 100 MiB and 500 frames, with at most 16,383
pixels on either axis of each frame. The 50 megapixel limit counts all frames:
width × height × frame count. For TIFF and HEIF/AVIF collections, pixel limits
apply to the selected image. These limits apply before resizing, even when
requesting a small output. See [configuration](operations.md#configuration) to
adjust them.

## Inspect an image

`POST /api/inspect` validates the image and returns its metadata as JSON.

```sh
curl --fail-with-body http://localhost:3000/api/inspect \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.jpg
```

Example response for a JPEG whose orientation rotates it into portrait:

```json
{
  "mime_type": "image/jpeg",
  "byte_count": 123456,
  "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "width": 1200,
  "height": 800,
  "oriented_width": 800,
  "oriented_height": 1200,
  "frame_count": 1
}
```

`width` and `height` are the decoded dimensions of the selected image or frame.
HEIF/AVIF container rotation and mirroring are already applied during decoding.
`oriented_width` and `oriented_height` describe that frame after applying
orientation. Use the oriented dimensions when choosing output sizes.
`byte_count` and `sha256` refer
to the exact bytes you uploaded. Inspection decodes the image to check it
is usable, including the animation frames reported by the decoder; it does not
return a converted image. For collections, only the selected image is decoded.

`frame_count` is `1` for a still image. Animated images also include
`frame_delays_ms`, an array of frame durations in milliseconds, and `loop_count`,
the number of times to play the animation. A loop count of `0` means repeat
indefinitely; `1` means play once. Still images omit those two fields.

TIFF and HEIF/AVIF inputs also include `page_count` and `page_index`, including
single-image files. `page_count` counts top-level images, excluding embedded
thumbnails and auxiliary images. `page_index` is numbered from zero: it is `0`
for TIFF and identifies the primary image for HEIF/AVIF. These inputs report
`frame_count: 1`; pages are not converted into an animation.

MIME types are `image/bmp`, `image/tiff`, `image/heic`, `image/heif`, or
`image/avif` for the additional input formats. HEIC-specific and generic HEIF
brands determine whether a HEVC file reports `image/heic` or `image/heif`;
the filename extension does not affect this.

## Convert or resize an image

`POST /api/transform` returns one image in the requested format. Pass conversion
options in the query string and the original bytes in the request body.

| Parameter | Default | Meaning |
| --- | --- | --- |
| `format` | Required | `webp` or `jxl` |
| `width` | No width bound | Maximum output width, from 1 to 16,383 pixels |
| `height` | No height bound | Maximum output height, from 1 to 16,383 pixels |
| `quality` | `80` | Lossy WebP quality, integer from 0 to 100 |
| `distance` | `1.0` | Lossy JPEG XL distance, greater than 0 and at most 25; lower values give higher quality |
| `lossless` | `false` | Use lossless encoding |

For a WebP image up to 750 pixels wide:

```sh
curl --fail-with-body 'http://localhost:3000/api/transform?format=webp&width=750' \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.jpg -o photo.webp
```

To convert at full resolution, omit both dimensions:

```sh
curl --fail-with-body 'http://localhost:3000/api/transform?format=webp' \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.jpg -o full.webp
```

For a lossless JPEG XL image:

```sh
curl --fail-with-body 'http://localhost:3000/api/transform?format=jxl&lossless=true' \
  -H 'Content-Type: application/octet-stream' --data-binary @photo.png -o photo.jxl
```

### Animations

Convert an animated GIF with the same endpoint and options:

```sh
curl --fail-with-body 'http://localhost:3000/api/transform?format=webp&width=750' \
  -H 'Content-Type: application/octet-stream' --data-binary @animation.gif -o animation.webp

curl --fail-with-body 'http://localhost:3000/api/transform?format=jxl&width=750' \
  -H 'Content-Type: application/octet-stream' --data-binary @animation.gif -o animation.jxl
```

Each frame is resized separately. GIF frame offsets, transparency, and disposal
are applied before conversion, so partial frames produce the intended animation.
The output preserves frame order and loop behavior. The same applies when the
input is already an animated WebP or JPEG XL.

The libvips WebP and JPEG XL writers change frame delays of 10 ms or less to
100 ms for browser playback compatibility. For example, an intentional 10 ms
delay becomes 100 ms, slowing that frame by a factor of ten. Exact timing for
these short frames is not supported. Other delays are retained at millisecond
precision. WebP may merge identical consecutive frames while retaining their
combined duration, or produce a still image when every frame is identical.
Inspection and the response headers report the actual output frame count.

### Sizing and image quality

Width and height describe a bounding box for each oriented frame. The result fits
within both bounds while keeping its aspect ratio. Images are never cropped or
upscaled, so the output can be smaller than the requested dimensions.

Use `quality` only with WebP and `distance` only with JPEG XL. With
`lossless=true`, omit both. Unknown, repeated, empty, out-of-range, or incompatible
options return `400`. The default quality settings are starting points; compare
output quality and file size on your own images before choosing production settings.

Output images have their orientation applied, including mirrored EXIF
orientations. Color conversion targets sRGB using the embedded ICC profile
when present, or the decoder's color-space interpretation otherwise.
EXIF, GPS, XMP, IPTC, and other source metadata are removed; necessary output
color and animation playback information is retained. Transparency is preserved.
An entirely opaque alpha channel may be omitted.

The `lossless` option controls encoding. Resizing, color conversion, and
conversion to the output format's supported representation can still change
pixels from the original. The uploaded source bytes are unchanged.

### Color and HDR limitations

The service uses libvips's color conversion and codec behavior. It does not
provide custom tone mapping or reconstruct missing color information.

| Input | What to expect |
| --- | --- |
| SDR image with an embedded ICC profile | Converted to sRGB. Display P3 and DCI-P3 profiles are interpreted individually; colors outside sRGB's gamut cannot be retained. |
| Image without an ICC profile | Uses the decoder's color-space interpretation. This cannot reliably recover untagged P3 or other wide-gamut colors. |
| HEIF/AVIF with only NCLX color metadata | The packaged libvips version ignores NCLX profiles. Colors and brightness can be incorrect, including for P3, PQ, and HLG inputs. |
| HDR image | HDR preservation and reliable HDR-to-SDR tone mapping are not supported. Decoding may succeed while losing highlight detail or producing incorrect brightness. High bit depth and lossless encoding do not resolve this. |
| Image with an HDR gain map | The gain map is not preserved or applied by this pipeline. Where the decoder exposes an SDR base image, that image is used. |

Successful inspection confirms that an image decodes, not that its color or HDR
intent will be preserved. For reliable results with HDR or NCLX-only images,
prepare an SDR image with an embedded ICC profile before sending it to the service.

These limits follow the packaged libraries: see the
[HEIF loader](https://github.com/libvips/libvips/blob/v8.18.2/libvips/foreign/heifload.c#L674),
[ICC conversion](https://www.libvips.org/API/current/method.Image.icc_transform.html),
and the [WebP](https://github.com/libvips/libvips/blob/v8.18.2/libvips/foreign/webpsave.c#L296)
and [JPEG XL](https://github.com/libvips/libvips/blob/v8.18.2/libvips/foreign/jxlsave.c#L655)
animation writers.

### Response

A successful response has status `200` and contains the image bytes, with these
headers:

| Header | Meaning |
| --- | --- |
| `Content-Type` | `image/webp` or `image/jxl` |
| `Content-Length` | Output size in bytes |
| `X-Image-Width` | Output frame width in pixels |
| `X-Image-Height` | Output frame height in pixels |
| `X-Image-Frame-Count` | Number of output frames; `1` for a still image |
| `X-Image-SHA256` | SHA-256 of the output bytes, as lowercase hexadecimal |

Save the response body as a file or stream it into your storage. Check the
status before accepting a response: errors contain JSON, not image bytes.
Validate the size and checksum before publishing a stored image. The service
does not store or serve completed images; make a separate request for each
output you need.

## Errors

Errors use `application/problem+json` with `type`, `title`, `status`, and `detail`.
For example, when all processing slots are occupied:

```json
{
  "type": "urn:image-transform:error:busy",
  "title": "Service Unavailable",
  "status": 503,
  "detail": "All image processing slots are occupied."
}
```

Use `type` to identify an error in client code. All types begin with
`urn:image-transform:error:`; the table shows the suffixes.

| Status | Type | What to check |
| --- | --- | --- |
| `400` | `invalid-request`, `invalid-options` | Check required parameters, allowed values, and codec-specific options. |
| `404` | `not-found` | Check the endpoint URL. |
| `405` | `method-not-allowed` | Use `POST` for inspection and conversion. |
| `408` | `request-timeout` | The upload took too long. Check the connection and request timeout. |
| `413` | `payload-too-large` | Keep the input within `MAX_INPUT_BYTES`. |
| `415` | `unsupported-media-type`, `unsupported-format` | Send a supported image with `Content-Type: application/octet-stream`. |
| `422` | `invalid-image` | Use a valid, complete image. |
| `422` | `frame-limit` | Keep the input within `MAX_FRAMES`. |
| `422` | `image-dimensions`, `output-limit` | Check per-frame dimensions and the total pixel limit, or reduce the requested output size. |
| `500` | `internal` | Check service logs and resource limits; report persistent failures. |
| `503` | `busy` | Retry with backoff or reduce simultaneous requests. |
| `503` | `cancelled`, `unavailable` | Check service health before retrying. |
| `504` | `timeout` | Processing exceeded its deadline. Review image size, resource limits, and the timeout. |

The service does not queue or retry requests. After a timeout or cancellation,
processing may take a little longer to stop; during that time it still occupies
a slot. A disconnected client or an interrupted response may receive no complete
error body. Close response bodies promptly so connections and capacity can be reused.

Image bytes and metadata are omitted from errors and logs. All responses use
`Cache-Control: no-store`.

## Health

`GET /health` returns `200` with `{"status":"ok"}` when the service is ready.
Startup checks BMP, TIFF, HEIC, AVIF, and GIF decoding, plus still and animated
WebP/JPEG XL conversion.
A busy service can still be healthy; `503` with the `busy` error means its request
capacity is full.
See [troubleshooting](operations.md#check-health-and-troubleshoot).

## OpenAPI and generated clients

Use [openapi.json](../openapi.json) or `GET /api/openapi.json` to generate a
client. The contract uses OpenAPI 3.0.3 with operation IDs `inspectImage`,
`transformImage`, and `getHealth`. Routes have no version prefix; `info.version`
identifies the software release. Keep your generated client and its schema
snapshot together when upgrading.

For Go clients generated with `oapi-codegen`, use
`InspectImageWithBodyWithResponse` for inspection and `TransformImageWithBody`
for conversion. The latter lets you stream the binary response and close its
body after reading it. The generated `WithResponse` helper buffers the whole
image in memory. Compatible generator and runtime versions are pinned in
[the client check](../scripts/check-client.sh).
