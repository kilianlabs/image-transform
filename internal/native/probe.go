package native

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"

	"github.com/kilianlabs/image-transform/internal/imageproc"
)

// Real input files keep readiness independent of optional HEIF/AVIF encoders.
//
//go:embed testdata/sample.bmp testdata/sample.tiff testdata/sample.heic testdata/sample.avif
var probeInputs embed.FS

// Probe exercises both encoders and decoders through the production adapter.
func (p *Processor) Probe(ctx context.Context, dir string) error {
	work, err := os.MkdirTemp(dir, "image-transform-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: uint8(x * 30), A: uint8(y * 80)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}}
	first, second := image.NewPaletted(img.Bounds(), palette), image.NewPaletted(img.Bounds(), palette)
	first.SetColorIndex(1, 1, 1)
	second.SetColorIndex(2, 2, 2)
	var animation bytes.Buffer
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{7, 13}, LoopCount: 0}); err != nil {
		return err
	}
	// Probe limits are independent of operator limits (which may be tiny).
	probe := &Processor{limits: imageproc.Limits{MaxPixels: 1000, MaxAxis: 16383, MaxFrames: 500, MaxOutputBytes: 1 << 20}}
	inputs := [][]byte{buf.Bytes(), animation.Bytes()}
	entries, err := probeInputs.ReadDir("testdata")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := probeInputs.ReadFile("testdata/" + entry.Name())
		if err != nil {
			return err
		}
		inputs = append(inputs, data)
	}
	for _, data := range inputs {
		source := filepath.Join(work, "source")
		if err := os.WriteFile(source, data, 0600); err != nil {
			return err
		}
		for _, format := range []string{"webp", "jxl"} {
			destination := filepath.Join(work, format)
			_, err := probe.Transform(ctx, source, destination, imageproc.Options{Format: format, Quality: 80, Distance: 1})
			if err != nil {
				return errors.New("codec encode/decode check failed")
			}
			if err := os.Remove(destination); err != nil {
				return err
			}
		}
	}
	return nil
}
