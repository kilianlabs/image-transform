# Image tags and upgrades

Use `ghcr.io/kilianlabs/image-transform` for container deployments. Images support
Linux AMD64 and ARM64; Docker chooses the matching platform automatically.

## Choose an image

Use a release tag for production. The `edge` tag follows development on `main`
and is useful for trying new changes. Version numbers in this table are examples
of the tag format, not a list of available releases.

| Tag | Meaning |
| --- | --- |
| `edge` | Most recent successful build from `main` |
| `0.1.0` | One specific release |
| `0.1` | Most recent published patch in the 0.1 series |
| `1` | Most recent published release in the 1.x series |
| `sha-<full commit SHA>` | Build from a particular source commit |

There is no `latest` tag or rolling `0` tag. Tags such as `edge`, `0.1`, and `1`
move as new images are published. Pin a digest to keep using an exact image:
`ghcr.io/kilianlabs/image-transform@sha256:<digest>`.

## Upgrade with Docker Compose

For a Compose stack using a registry image, such as the
[README example](../README.md#run-with-docker):

1. Set the service's `image` to your chosen release tag or digest.
2. Pull the image and recreate the service:

   ```sh
   docker compose pull image-transform
   docker compose up -d image-transform
   ```

3. Run `docker compose ps image-transform` and check that the container becomes
   healthy. Use `docker compose logs image-transform` to investigate startup failures.

Let active requests finish before replacing the container, or have your
application retry interrupted conversions. No service database or persistent
volume needs migration. Your application's stored originals and outputs are
unaffected.

If you generate an API client, compare the release's `openapi.json` with your
saved schema and regenerate when needed. Test conversion settings with your own
images after an upgrade; codec updates can change output bytes and file sizes.
The [API guide](api.md#openapi-and-generated-clients) explains client generation.

## Roll back

Set `image` back to the previous release tag or digest and repeat the pull and
recreate commands. Keep the previous image reference with your deployment
configuration so it is available when needed.

To build from source, see [Contributing](../CONTRIBUTING.md#local-setup).
