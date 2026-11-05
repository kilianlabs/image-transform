# Deployment and configuration

Start with the [Docker and Compose examples](../README.md#run-with-docker).
Image Transform processes images locally and needs no database, storage
credentials, or outbound network access while serving requests.

## Configuration

Set environment variables when starting the service. Invalid settings prevent
startup; configuration changes require a restart.

| Variable | Default | Purpose |
| --- | --- | --- |
| `HOST` | `0.0.0.0` in Docker; `127.0.0.1` locally | Listen address |
| `PORT` | `3000` | HTTP port |
| `TMPDIR` | `/tmp` in Docker; OS temporary directory locally | Writable directory for temporary image files |
| `MAX_INPUT_BYTES` | `104857600` | 100 MiB input limit |
| `MAX_OUTPUT_BYTES` | `268435456` | 256 MiB output limit |
| `MAX_PIXELS` | `50000000` | Maximum source pixels across animation frames, or in the selected collection image |
| `MAX_AXIS` | `16383` | Maximum width or height of each source frame; cannot exceed 16,383 |
| `MAX_FRAMES` | `500` | Maximum source frames, from 1 to 10,000 |
| `MAX_CONCURRENT` | `1` | Simultaneous image requests, including upload and response download |
| `MAX_INSPECT_CONCURRENT` | `MAX_CONCURRENT` | Maximum active inspections; 1 to `MAX_CONCURRENT` |
| `MAX_TRANSFORM_CONCURRENT` | `MAX_CONCURRENT` | Maximum active conversions; 1 to `MAX_CONCURRENT` |
| `VIPS_THREADS` | `2` | Number of libvips processing threads |
| `REQUEST_TIMEOUT` | `120s` | Upload and processing deadline; accepts durations such as `30s` or `2m`, up to `1h` |

For example, to accept inputs up to 25 MiB and 20 megapixels across all frames:

```yaml
services:
  image-transform:
    environment:
      MAX_INPUT_BYTES: "26214400"
      MAX_PIXELS: "20000000"
```

Add this `environment` block to the service in your Compose file. Source limits
apply before resizing, so a small thumbnail request still needs an input within
those limits.

## Networking and temporary storage

Keep the unauthenticated API on a trusted private network. Applications in the
same Compose network can use `http://image-transform:3000` without publishing a
host port. For local access, bind the host port to `127.0.0.1` as shown in the
README. See [security guidance](../SECURITY.md#run-securely) for other deployments.

The container runs as user and group ID 1000 and supports a read-only root
filesystem. Provide a writable `/tmp` tmpfs, or set `TMPDIR` to another directory
writable by that user. No persistent volume is needed.

Uploads and output files are temporary. They are removed after each request
once processing and response delivery have ended. Files from cancelled requests
are removed when processing stops. A tmpfs also clears when the container stops,
including after an abrupt exit. Store your originals and completed outputs in
your application; Image Transform does not retain an image library.

## Size resources for your workload

The Docker examples and supplied Compose file use two CPUs, 2 GiB of memory,
a 2 GiB `/tmp` tmpfs, and a limit of 128 processes. Temporary storage counts
toward the container's memory limit; it is not an additional 2 GiB allowance.

Image dimensions, frame count, format, and conversion options affect memory and
CPU use. Compressed file size alone does not predict processing cost, and the output
size limit does not cap memory used during encoding. Test representative images
before raising input or concurrency limits. If the container runs out of memory,
increase its memory allocation or reduce the image limits.

`MAX_CONCURRENT` limits whole image requests. Each operation also has its own
active limit. Requests that exceed either limit receive `503 busy` immediately,
before the body is copied to temporary storage. The service has no waiting queue.
Setting only `MAX_CONCURRENT` retains shared capacity.

To allow one inspection alongside one conversion, add these settings to the
service's Compose configuration:

```yaml
environment:
  MAX_CONCURRENT: "2"
  MAX_INSPECT_CONCURRENT: "1"
  MAX_TRANSFORM_CONCURRENT: "1"
```

Conversions cannot borrow the inspection slot in this configuration. CPU, memory
and tmpfs remain shared. Applications retain pending jobs and retry explicit
`urn:image-transform:error:busy` responses with backoff and jitter. Health and
OpenAPI requests remain independent of image capacity.

The [concurrency benchmark](benchmarks/concurrency.md) compares the shared
slot with 2/1/1 admission and one or two libvips threads under these container
limits. Use its reproducible workload as a starting point, then measure your own
images and hardware.

## Timeouts and cancellation

Set client and proxy timeouts to allow for upload, conversion, and downloading
the result. `REQUEST_TIMEOUT` covers upload and processing and defaults to two
minutes. HTTP reads use the same duration; writes allow five additional seconds
for response delivery. Slow uploads and downloads also have transport deadlines,
so a connection can close before a complete response arrives.

When a request times out or its client disconnects, the service asks processing
to stop and discards any late result. An encoder may need to finish its current
work first. Until it stops, the request still occupies a processing slot. Avoid
immediate retry loops after a timeout.

## Check health and troubleshoot

Use `GET /health` or Docker's health status to check readiness:

```sh
curl --fail-with-body http://localhost:3000/health
docker compose ps image-transform
docker compose logs image-transform
```

The curl command assumes a localhost port is published. A healthy response is
`{"status":"ok"}`. Startup verifies BMP, TIFF, HEIC, AVIF, and GIF decoding, plus
still and animated WebP/JPEG XL encoding and decoding. If those checks fail,
the service exits instead of accepting requests.

| Symptom | What to check |
| --- | --- |
| The container will not start | Inspect logs for invalid configuration, an unwritable temporary directory, or failed codec checks. |
| Requests return `503` with `busy` | Reduce simultaneous requests or wait for current work to finish. A healthy service can be busy. |
| Requests return `413` or `422` | Check the [API error type](api.md#errors), input size, dimensions, animation, and output limits. |
| Requests time out | Check upload speed and CPU usage. Reduce image size or adjust the request timeout and resource limits. |
| The container exits during processing | Check whether Docker reports an out-of-memory exit. Increase memory or lower image and concurrency limits. |
| Requests repeatedly return `500` | Check temporary storage space and permissions, then report the failure with a synthetic image if possible. |

### Logs

JSON logs go to standard output and include startup, the listening address,
shutdown, and processing failures. Failure entries identify the operation,
error type, and HTTP status. Image bytes, filenames, metadata, and raw codec
errors are omitted. Monitor container memory and CPU alongside the frequency
of `busy`, `timeout`, and `internal` errors.

## Stop or remove the service

Stop the container normally with `docker compose stop image-transform`. The
service stops accepting image requests, cancels active work, and allows five
seconds for connections to drain. It waits for native processing to stop before
exiting; the supplied Compose configuration allows ten seconds before Docker
forcibly stops it.

Allow requests to finish before stopping if you want to avoid interrupted
conversions. There is no persistent service data to migrate or remove. For
image upgrades and rollback, see [image tags and upgrades](releases.md).
