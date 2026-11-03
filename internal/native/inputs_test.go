package native

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
	"golang.org/x/image/bmp"
)

func inputFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAdditionalInputFormats(t *testing.T) {
	for _, fixture := range []struct {
		name, mime    string
		width, height int
		oriented      bool
		alpha         bool
	}{
		{"sample.bmp", "image/bmp", 24, 16, false, false},
		{"sample.tiff", "image/tiff", 24, 16, false, true},
		{"sample.heic", "image/heic", 24, 16, false, true},
		{"sample.heif", "image/heif", 24, 16, false, true},
		{"sample.avif", "image/avif", 24, 16, false, true},
		{"oriented.tiff", "image/tiff", 24, 16, true, true},
		// libheif applies HEIF container orientation during load.
		{"oriented.heic", "image/heic", 16, 24, true, true},
		{"oriented.avif", "image/avif", 16, 24, true, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data := inputFixture(t, fixture.name)
			source := save(t, data)
			info, err := engine.Inspect(t.Context(), source)
			if err != nil || info.MIMEType != fixture.mime || info.Width != fixture.width || info.Height != fixture.height || info.FrameCount != 1 {
				t.Fatalf("inspect: %+v %v", info, err)
			}
			width, height := 24, 16
			if fixture.oriented {
				width, height = 16, 24
			}
			if info.OrientedWidth != width || info.OrientedHeight != height {
				t.Fatalf("orientation: %+v", info)
			}
			for _, format := range []string{"webp", "jxl"} {
				for _, bound := range []int{0, 8} {
					dest := filepath.Join(t.TempDir(), "output")
					meta, err := engine.Transform(t.Context(), source, dest, imageproc.Options{Format: format, Width: bound, Height: bound, Lossless: true})
					if err != nil {
						t.Fatal(err)
					}
					w, h := width, height
					if bound > 0 {
						w, h = 8, 5
						if fixture.oriented {
							w, h = 5, 8
						}
					}
					if meta.Width != w || meta.Height != h || meta.FrameCount != 1 || meta.PageCount != 0 || meta.PageIndex != nil {
						t.Fatal(meta)
					}
					ref := output(t, dest)
					if (fixture.alpha && !ref.HasAlpha()) || ref.HasExif() || len(ref.GetBlob("xmp-data")) != 0 || ref.Orientation() > 1 {
						t.Error("transparency, orientation or metadata handling changed")
					}
					ref.Close()
				}
			}
			after, err := os.ReadFile(source)
			if err != nil || sha256.Sum256(data) != sha256.Sum256(after) {
				t.Fatal("source changed", err)
			}
		})
	}
	vips.AssertNoLeaks(t)
}

func TestCollectionSelection(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		page, channel int
	}{
		{"collection.tiff", 0, 0},
		{"collection.heic", 1, 1},
		{"collection.avif", 1, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := save(t, inputFixture(t, fixture.name))
			info, err := engine.Inspect(t.Context(), source)
			if err != nil || info.FrameCount != 1 || info.PageCount != 2 || info.PageIndex == nil || *info.PageIndex != fixture.page || info.LoopCount != nil {
				t.Fatalf("selection: %+v %v", info, err)
			}
			// Only the selected image is decoded and counted toward pixel limits.
			limited := &Processor{limits: imageproc.Limits{MaxPixels: 24 * 16, MaxAxis: 24, MaxFrames: 1, MaxOutputBytes: 1 << 20}}
			for _, format := range []string{"webp", "jxl"} {
				dest := filepath.Join(t.TempDir(), "out")
				meta, err := limited.Transform(t.Context(), source, dest, imageproc.Options{Format: format, Lossless: true})
				if err != nil || meta.Width != 24 || meta.Height != 16 || meta.FrameCount != 1 {
					t.Fatalf("%+v %v", meta, err)
				}
				ref := output(t, dest)
				pixel, err := ref.GetPoint(12, 8)
				ref.Close()
				if err != nil || len(pixel) < 3 || pixel[fixture.channel] < 250 || pixel[1-fixture.channel] > 5 {
					t.Fatalf("wrong selected image: %v %v", pixel, err)
				}
			}
		})
	}
	vips.AssertNoLeaks(t)
}

func TestAdditionalInputLimitsAndCorruption(t *testing.T) {
	limited := &Processor{limits: imageproc.Limits{MaxPixels: 24*16 - 1, MaxAxis: 16383, MaxFrames: 500, MaxOutputBytes: 1 << 20}}
	for _, name := range []string{"sample.bmp", "sample.tiff", "sample.heic", "sample.avif"} {
		t.Run(name, func(t *testing.T) {
			data := inputFixture(t, name)
			if _, err := limited.Inspect(t.Context(), save(t, data)); !errors.Is(err, imageproc.ErrDimensions) {
				t.Fatal(err)
			}
			if _, err := engine.Inspect(t.Context(), save(t, data[:len(data)/2])); !errors.Is(err, imageproc.ErrInvalid) {
				t.Fatalf("truncation accepted: %v", err)
			}
		})
	}
	vips.AssertNoLeaks(t)
}

func TestHEIFBrandsAndImageSequences(t *testing.T) {
	// AVIF may advertise the generic HEIF major brand and AVIF compatibility.
	avif := inputFixture(t, "sample.avif")
	copy(avif[8:12], "mif1")
	info, err := engine.Inspect(t.Context(), save(t, avif))
	if err != nil || info.MIMEType != "image/avif" {
		t.Fatalf("compatible AVIF brand: %+v %v", info, err)
	}
	// Layered HEIC still-image brands must not be mistaken for sequences.
	for _, brand := range []string{"heim", "heis"} {
		data := inputFixture(t, "sample.heic")
		copy(data[8:12], brand)
		info, err := engine.Inspect(t.Context(), save(t, data))
		if err != nil || info.MIMEType != "image/heic" || info.FrameCount != 1 {
			t.Fatalf("still brand %s: %+v %v", brand, info, err)
		}
	}
	for _, brand := range []string{"avis", "msf1", "hevc", "hevx", "hevm", "hevs", "avcs"} {
		for _, offset := range []int{8, 16} {
			data := inputFixture(t, "sample.heic")
			copy(data[offset:offset+4], brand)
			path := save(t, data)
			if _, err := engine.Inspect(t.Context(), path); !errors.Is(err, imageproc.ErrUnsupported) {
				t.Fatalf("sequence brand %s at %d: %v", brand, offset, err)
			}
			if _, err := engine.Transform(t.Context(), path, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: "webp", Quality: 80}); !errors.Is(err, imageproc.ErrUnsupported) {
				t.Fatal(err)
			}
		}
	}
	vips.AssertNoLeaks(t)
}

func TestBMPPaletteAndTopDownRows(t *testing.T) {
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	indexed := image.NewPaletted(image.Rect(0, 0, 3, 2), palette)
	indexed.Pix = []byte{0, 0, 0, 1, 1, 1}
	rgb := image.NewRGBA(indexed.Bounds())
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			rgb.Set(x, y, indexed.At(x, y))
		}
	}
	for _, img := range []image.Image{indexed, rgb} {
		var buf bytes.Buffer
		if err := bmp.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		for _, topDown := range []bool{false, true} {
			data := bytes.Clone(buf.Bytes())
			if topDown {
				binary.LittleEndian.PutUint32(data[22:26], 0xfffffffe) // signed -2
				offset := int(binary.LittleEndian.Uint32(data[10:14]))
				stride := (len(data) - offset) / 2
				copy(data[offset:], buf.Bytes()[offset+stride:])
				copy(data[offset+stride:], buf.Bytes()[offset:offset+stride])
			}
			for _, format := range []string{"webp", "jxl"} {
				dest := filepath.Join(t.TempDir(), "out")
				if _, err := engine.Transform(t.Context(), save(t, data), dest, imageproc.Options{Format: format, Lossless: true}); err != nil {
					t.Fatal(err)
				}
				ref := output(t, dest)
				top, topErr := ref.GetPoint(1, 0)
				bottom, bottomErr := ref.GetPoint(1, 1)
				ref.Close()
				if topErr != nil || bottomErr != nil || top[0] != 255 || top[2] != 0 || bottom[0] != 0 || bottom[2] != 255 {
					t.Fatalf("BMP rows: %v %v (%v %v)", top, bottom, topErr, bottomErr)
				}
			}
		}
	}
	vips.AssertNoLeaks(t)
}
