package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/testimage"
)

func TestAnimatedCodecMatrix(t *testing.T) {
	data := testimage.GIF(17, 13, []int{4, 7, 13}, 2)
	sources := map[string]string{"gif": save(t, data)}
	for _, format := range []string{"webp", "jxl"} {
		path := filepath.Join(t.TempDir(), format)
		_, err := engine.Transform(t.Context(), sources["gif"], path, imageproc.Options{Format: format, Lossless: true})
		if err != nil {
			t.Fatal(err)
		}
		sources[format] = path
	}
	for name, path := range sources {
		info, err := engine.Inspect(t.Context(), path)
		if err != nil || info.MIMEType != "image/"+name || info.Width != 17 || info.Height != 13 || info.FrameCount != 3 || info.LoopCount == nil || *info.LoopCount != 3 || !slices.Equal(info.FrameDelaysMS, []int{40, 70, 130}) {
			t.Fatalf("%s: %+v %v", name, info, err)
		}
		before, _ := os.ReadFile(path)
		for _, format := range []string{"webp", "jxl"} {
			for _, size := range []int{0, 7, 100} {
				for _, lossless := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s-%s-%d-lossless=%v", name, format, size, lossless), func(t *testing.T) {
						dest := filepath.Join(t.TempDir(), "output")
						meta, err := engine.Transform(t.Context(), path, dest, imageproc.Options{Format: format, Width: size, Height: size, Quality: 80, Distance: 1, Lossless: lossless})
						if err != nil {
							t.Fatal(err)
						}
						width, height := 17, 13
						if size == 7 {
							width, height = 7, 5
						}
						if meta.Width != width || meta.Height != height || meta.OrientedHeight != height || meta.FrameCount != 3 || !slices.Equal(meta.FrameDelaysMS, info.FrameDelaysMS) || *meta.LoopCount != 3 {
							t.Fatal(meta)
						}
						decoded, _, err := engine.load(t.Context(), dest)
						if err != nil {
							t.Fatal(err)
						}
						defer decoded.Close()
						// Check both frame edges: resizing the whole strip blends
						// colors across boundaries and changes the frame geometry.
						for frame := 0; frame < 3; frame++ {
							for _, y := range []int{frame * height, (frame+1)*height - 1} {
								pixel, err := decoded.GetPoint(width-1, y)
								if err != nil || len(pixel) != 4 || pixel[frame] < 240 || pixel[(frame+1)%3] > 15 || pixel[(frame+2)%3] > 15 || pixel[3] != 255 {
									t.Fatalf("frame %d edge: %v %v", frame, pixel, err)
								}
							}
						}
					})
				}
			}
		}
		after, _ := os.ReadFile(path)
		if sha256.Sum256(before) != sha256.Sum256(after) {
			t.Fatal("source changed")
		}
	}
	vips.AssertNoLeaks(t)
}

func TestAnimationTimingLoopsAndDuplicateFrames(t *testing.T) {
	for _, loop := range []int{-1, 0, 2} {
		for _, duplicates := range []string{"none", "all", "adjacent"} {
			data := testimage.GIF(16, 12, []int{0, 1, 2}, loop)
			if duplicates != "none" {
				animation, err := gif.DecodeAll(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				for i, frame := range animation.Image {
					if duplicates == "all" || i == 1 {
						copy(frame.Pix, animation.Image[0].Pix)
					}
				}
				var buf bytes.Buffer
				if err := gif.EncodeAll(&buf, animation); err != nil {
					t.Fatal(err)
				}
				data = buf.Bytes()
			}
			for _, format := range []string{"webp", "jxl"} {
				meta, err := engine.Transform(t.Context(), save(t, data), filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: format, Lossless: true})
				if err != nil {
					t.Fatal(err)
				}
				if duplicates == "all" && format == "webp" {
					if meta.FrameCount != 1 || meta.LoopCount != nil || len(meta.FrameDelaysMS) != 0 {
						t.Fatal(meta)
					}
					continue
				}
				plays := loop + 1
				if loop == -1 {
					plays = 1
				}
				if loop == 0 {
					plays = 0
				}
				delays := []int{100, 100, 20}
				if duplicates == "adjacent" && format == "webp" {
					delays = []int{200, 20}
				}
				if meta.FrameCount != len(delays) || meta.LoopCount == nil || *meta.LoopCount != plays || !slices.Equal(meta.FrameDelaysMS, delays) {
					t.Fatal(meta)
				}
			}
		}
	}
	vips.AssertNoLeaks(t)
}

func TestGIFDisposalAndTransparency(t *testing.T) {
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	animation := &gif.GIF{LoopCount: 0, Delay: []int{4, 7, 13, 9}, Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious, gif.DisposalNone}}
	for i, rect := range []image.Rectangle{image.Rect(0, 0, 8, 8), image.Rect(2, 2, 6, 6), image.Rect(3, 3, 5, 5), image.Rect(6, 6, 8, 8)} {
		frame := image.NewPaletted(rect, palette)
		for j := range frame.Pix {
			frame.Pix[j] = uint8(i%3 + 1)
		}
		animation.Image = append(animation.Image, frame)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, animation); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"webp", "jxl"} {
		dest := filepath.Join(t.TempDir(), "out")
		if _, err := engine.Transform(t.Context(), save(t, buf.Bytes()), dest, imageproc.Options{Format: format, Lossless: true}); err != nil {
			t.Fatal(err)
		}
		ref, _, err := engine.load(t.Context(), dest)
		if err != nil {
			t.Fatal(err)
		}
		for frame, want := range [][]float64{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {0, 0, 0, 0}} {
			pixel, err := ref.GetPoint(3, frame*8+3)
			if err != nil || len(pixel) != 4 || pixel[3] != want[3] || (want[3] != 0 && !slices.Equal(pixel, want)) {
				t.Fatalf("%s frame %d: %v want %v (%v)", format, frame, pixel, want, err)
			}
		}
		ref.Close()
	}
	vips.AssertNoLeaks(t)
}

func TestAnimationLimitsAndCleanup(t *testing.T) {
	path := save(t, testimage.GIF(17, 13, []int{4, 7, 13}, 0))
	sources := []string{path}
	for _, format := range []string{"webp", "jxl"} {
		dest := filepath.Join(t.TempDir(), format)
		if _, err := engine.Transform(t.Context(), path, dest, imageproc.Options{Format: format, Lossless: true}); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, dest)
	}
	for _, limit := range []struct {
		pixels int64
		frames int
		want   error
	}{{17 * 13 * 3, 3, nil}, {17*13*3 - 1, 3, imageproc.ErrDimensions}, {17 * 13 * 3, 2, imageproc.ErrFrames}} {
		p := &Processor{limits: imageproc.Limits{MaxPixels: limit.pixels, MaxFrames: limit.frames, MaxAxis: 16383, MaxOutputBytes: 1 << 20}}
		for _, source := range sources {
			if _, err := p.Inspect(t.Context(), source); !errors.Is(err, limit.want) {
				t.Fatalf("inspect: %v want %v", err, limit.want)
			}
			if _, err := p.Transform(t.Context(), source, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: "webp", Quality: 80}); !errors.Is(err, limit.want) {
				t.Fatalf("transform: %v want %v", err, limit.want)
			}
		}
		vips.AssertNoLeaks(t)
	}
	// The axis limit applies to each frame, not the height of libvips's strip.
	info, err := engine.Inspect(t.Context(), save(t, testimage.GIF(1, 8200, []int{4, 7}, 0)))
	if err != nil || info.Height != 8200 || info.FrameCount != 2 {
		t.Fatalf("%+v %v", info, err)
	}
	limited := &Processor{limits: imageproc.Limits{MaxPixels: 50_000_000, MaxFrames: 500, MaxAxis: 16383, MaxOutputBytes: 10}}
	for _, format := range []string{"webp", "jxl"} {
		if _, err := limited.Transform(t.Context(), path, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: format, Lossless: true}); !errors.Is(err, imageproc.ErrOutputLimit) {
			t.Fatalf("%s output limit: %v", format, err)
		}
		vips.AssertNoLeaks(t)
	}
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		if i%3 == 0 {
			cancel()
		}
		_, err := engine.Transform(ctx, path, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: []string{"webp", "jxl"}[i%2], Lossless: true})
		cancel()
		if (i%3 == 0 && !errors.Is(err, context.Canceled)) || (i%3 != 0 && err != nil) {
			t.Fatal(err)
		}
		vips.AssertNoLeaks(t)
	}
}

func TestAnimatedOrientationColorAndPrivacy(t *testing.T) {
	source := save(t, testimage.GIF(17, 13, []int{4, 7, 13}, 0))
	for _, inputFormat := range []string{"webp", "jxl"} {
		ref, _, err := engine.load(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		width, height := 17, 13
		if inputFormat == "webp" {
			if err := ref.SetOrientation(6); err != nil {
				t.Fatal(err)
			}
			width, height = 13, 17
		}
		ref.SetString("exif-ifd0-Artist", "private artist")
		ref.SetBlob("xmp-data", []byte("private location"))
		if err := ref.TransformICCProfile("srgb"); err != nil {
			t.Fatal(err)
		}
		var data []byte
		if inputFormat == "webp" {
			params := vips.NewWebpExportParams()
			params.Keep, params.Lossless = vips.ForeignKeepAll, true
			data, _, err = ref.ExportWebp(params)
		} else {
			params := vips.NewJxlExportParams()
			params.Keep, params.Lossless, params.Distance = vips.ForeignKeepAll, true, 0
			data, _, err = ref.ExportJxl(params)
		}
		ref.Close()
		if err != nil {
			t.Fatal(err)
		}
		original, err := engine.Inspect(t.Context(), save(t, data))
		if err != nil {
			t.Fatal(err)
		}
		if original.OrientedWidth != width || original.OrientedHeight != height {
			t.Fatalf("%s fixture orientation: %+v", inputFormat, original)
		}
		for _, format := range []string{"webp", "jxl"} {
			dest := filepath.Join(t.TempDir(), "out")
			meta, err := engine.Transform(t.Context(), save(t, data), dest, imageproc.Options{Format: format, Lossless: true})
			if err != nil {
				t.Fatalf("%s -> %s: %v", inputFormat, format, err)
			}
			if meta.Width != width || meta.Height != height || meta.FrameCount != 3 {
				t.Fatal(meta)
			}
			converted, _, err := engine.load(t.Context(), dest)
			if err != nil {
				t.Fatal(err)
			}
			if converted.HasExif() || len(converted.GetBlob("xmp-data")) != 0 || converted.Orientation() > 1 || !converted.HasICCProfile() {
				t.Fatal("orientation, privacy, or color profile changed")
			}
			converted.Close()
		}
	}
	vips.AssertNoLeaks(t)
}

func TestStillGIF(t *testing.T) {
	still := save(t, testimage.GIF(8, 6, []int{7}, -1))
	info, err := engine.Inspect(t.Context(), still)
	if err != nil || info.FrameCount != 1 || info.LoopCount != nil || len(info.FrameDelaysMS) != 0 {
		t.Fatalf("%+v %v", info, err)
	}
	for _, format := range []string{"webp", "jxl"} {
		meta, err := engine.Transform(t.Context(), still, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: format, Lossless: true})
		if err != nil || meta.FrameCount != 1 || meta.Width != 8 || meta.Height != 6 {
			t.Fatalf("%+v %v", meta, err)
		}
	}
	vips.AssertNoLeaks(t)
}

func TestDecoderRecoveredGIF(t *testing.T) {
	data := testimage.GIF(16, 12, []int{4, 7, 13}, 0)
	// libnsgif can recover completed frames from an incomplete stream.
	for _, size := range []int{len(data) / 2, len(data) - 1} {
		path := save(t, data[:size])
		info, err := engine.Inspect(t.Context(), path)
		if err != nil || info.FrameCount < 1 || info.FrameCount > 3 {
			t.Fatalf("truncated at %d: %+v %v", size, info, err)
		}
		for _, format := range []string{"webp", "jxl"} {
			meta, err := engine.Transform(t.Context(), path, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: format, Lossless: true})
			if err != nil || meta.FrameCount != info.FrameCount || meta.Width != 16 || meta.Height != 12 {
				t.Fatalf("recovered %s: %+v %v", format, meta, err)
			}
		}
	}
	vips.AssertNoLeaks(t)
}
