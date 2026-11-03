package native

import (
	"context"
	"io"
	"os"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/gabriel-vasile/mimetype"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

// Detection restricts the native loaders; decoding determines image validity.
// A bounded header check cannot guarantee that every animation is identified.
func detectFormat(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	header, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	detected := mimetype.Detect(header)
	mime := detected.String()
	// mimetype does not recognize BigTIFF; govips recognizes both TIFF variants.
	if vips.DetermineImageType(header) == vips.ImageTypeTIFF {
		mime = "image/tiff"
	}
	// HEIF and AVIF share the MP4 container. libheif also understands their
	// compatible brands and distinguishes AVIF still images from sequences.
	for parent := detected; parent != nil; parent = parent.Parent() {
		if parent.Is("video/mp4") {
			mime = heifMIME(header)
			break
		}
	}
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/jxl",
		"image/bmp", "image/tiff", "image/heic", "image/heif", "image/avif":
		return mime, nil
	default:
		return "", imageproc.ErrUnsupported
	}
}
