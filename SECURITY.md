# Security

## Reporting vulnerabilities

Report security issues through
[GitHub private vulnerability reporting](https://github.com/kilianlabs/image-transform/security/advisories/new).
Include the affected version or image digest, steps to reproduce the issue,
and its impact. Use synthetic images where possible and remove personal
metadata. Omit credentials and avoid public disclosure of unpatched vulnerabilities.

## Run securely

Image Transform has no authentication or rate limiting. Restrict access to the
applications that need it on a trusted private network. If access crosses that
network boundary, provide authentication, TLS, and rate limits at a proxy.

Use container memory, CPU, process, and temporary-storage limits, and keep the
image updated. Upload and pixel limits help control resource use. A processing
timeout requests cancellation, but an encoder may take longer to stop. See
[deployment and configuration](docs/operations.md) for sizing and timeout behavior.

Images are processed locally without fetching remote URLs or using storage
credentials. Temporary image files are removed after processing and delivery
end. Private metadata is stripped from converted images. Your application
controls access to its stored originals and outputs.

Security fixes target the latest stable release. Choose a release tag or image
digest for production; `edge` follows ongoing development.
