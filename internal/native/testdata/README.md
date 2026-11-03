# Input fixtures

These are original synthetic images, generated for the tests with govips and
Go's BMP encoder. They contain no user images or personal metadata.

- `sample.*`: a 24 × 16 color gradient, with transparency where the format supports it.
- `oriented.*`: the same image with orientation 6, an sRGB profile, and dummy
  artist/XMP metadata to check rotation, color, and metadata removal.
- `collection.*`: two 24 × 16 images, red then green. HEIF/AVIF marks the second
  image as primary; TIFF conversion selects the first page.
- `sample.heif`: the HEVC sample with generic `mif1` brands, to check HEIF
  detection independently of the filename and HEIC-specific brands.

The small samples are also used for startup decoder checks and container tests.
HEVC/AV1 encoders are not required in the runtime image.
