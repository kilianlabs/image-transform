package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/testimage"
)

var engine *Processor

func TestMain(m *testing.M) {
	var err error
	engine, err = Start(imageproc.Limits{MaxPixels: 50_000_000, MaxAxis: 16383, MaxFrames: 500, MaxOutputBytes: 256 << 20}, 2)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	Shutdown()
	os.Exit(code)
}
func save(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func load(t *testing.T, data []byte) *vips.ImageRef {
	t.Helper()
	ref, err := vips.NewImageFromBuffer(data)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func output(t *testing.T, path string) *vips.ImageRef {
	t.Helper()
	ref, err := vips.NewImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestCodecMatrix(t *testing.T) {
	if err := engine.Probe(t.Context(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	base := testimage.PNG(96, 64, true)
	ref := load(t, base)
	jpeg, _, err := ref.ExportJpeg(vips.NewJpegExportParams())
	if err != nil {
		t.Fatal(err)
	}
	webp, _, err := ref.ExportWebp(vips.NewWebpExportParams())
	if err != nil {
		t.Fatal(err)
	}
	jxl, _, err := ref.ExportJxl(vips.NewJxlExportParams())
	if err != nil {
		t.Fatal(err)
	}
	ref.Close()
	for name, data := range map[string][]byte{"png": base, "jpeg": jpeg, "webp": webp, "jxl": jxl} {
		for _, format := range []string{"webp", "jxl"} {
			t.Run(name+"-"+format, func(t *testing.T) {
				source := save(t, data)
				before := sha256.Sum256(data)
				dest := filepath.Join(t.TempDir(), "result")
				meta, err := engine.Transform(t.Context(), source, dest, imageproc.Options{Format: format, Width: 32, Height: 32, Quality: 80, Distance: 1})
				if err != nil {
					t.Fatal(err)
				}
				if meta.Width != 32 || meta.Height != 21 {
					t.Fatal(meta)
				}
				converted := output(t, dest)
				defer converted.Close()
				if converted.Width() != 32 || converted.Height() != 21 {
					t.Fatal(converted.Metadata())
				}
				if name != "jpeg" && !converted.HasAlpha() {
					t.Fatal("transparency lost")
				}
				after, _ := os.ReadFile(source)
				if before != sha256.Sum256(after) {
					t.Fatal("source changed")
				}
			})
		}
	}
	vips.AssertNoLeaks(t)
}

func TestOrientationICCAndMetadata(t *testing.T) {
	ref := load(t, testimage.PNG(80, 40, false))
	if err := ref.SetOrientation(6); err != nil {
		t.Fatal(err)
	}
	ref.SetString("exif-ifd0-Artist", "sensitive artist")
	ref.SetBlob("xmp-data", []byte("private GPS data"))
	if err := ref.TransformICCProfile("srgb"); err != nil {
		t.Fatal(err)
	}
	encoded, _, err := ref.ExportJpeg(vips.NewJpegExportParams())
	ref.Close()
	if err != nil {
		t.Fatal(err)
	}
	source := save(t, encoded)
	info, err := engine.Inspect(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 80 || info.Height != 40 || info.OrientedWidth != 40 || info.OrientedHeight != 80 {
		t.Fatal(info)
	}
	for _, format := range []string{"webp", "jxl"} {
		dest := filepath.Join(t.TempDir(), "result")
		meta, err := engine.Transform(t.Context(), source, dest, imageproc.Options{Format: format, Quality: 80, Distance: 1})
		if err != nil {
			t.Fatal(err)
		}
		if meta.Width != 40 || meta.Height != 80 {
			t.Fatal(meta)
		}
		converted := output(t, dest)
		if converted.HasExif() || len(converted.GetBlob("xmp-data")) != 0 || converted.Orientation() > 1 {
			t.Fatal("metadata was retained")
		}
		if !converted.HasICCProfile() {
			t.Fatal("converted color profile lost")
		}
		converted.Close()
	}
	vips.AssertNoLeaks(t)
}

func TestLosslessAndNoUpscale(t *testing.T) {
	data := testimage.PNG(12, 8, true)
	source := save(t, data)
	for _, format := range []string{"webp", "jxl"} {
		dest := filepath.Join(t.TempDir(), "result")
		meta, err := engine.Transform(t.Context(), source, dest, imageproc.Options{Format: format, Width: 500, Height: 500, Quality: 80, Distance: 1, Lossless: true})
		if err != nil {
			t.Fatal(err)
		}
		if meta.Width != 12 || meta.Height != 8 {
			t.Fatal("upscaled", meta)
		}
		original := load(t, data)
		converted := output(t, dest)
		for y := 0; y < 8; y++ {
			for x := 0; x < 12; x++ {
				a, err := original.GetPoint(x, y)
				if err != nil {
					t.Fatal(err)
				}
				b, err := converted.GetPoint(x, y)
				if err != nil {
					t.Fatal(err)
				}
				if len(a) != len(b) {
					t.Fatal("alpha changed")
				}
				for band := range a {
					if band < 3 && a[3] == 0 {
						continue
					}
					if a[band] != b[band] {
						t.Fatalf("%s pixel %d,%d: %v != %v", format, x, y, a, b)
					}
				}
			}
		}
		original.Close()
		converted.Close()
	}
	vips.AssertNoLeaks(t)
}

func TestUnsupportedAndInvalidInputs(t *testing.T) {
	base := testimage.PNG(16, 16, true)
	var chunk bytes.Buffer
	binary.Write(&chunk, binary.BigEndian, uint32(8))
	chunk.WriteString("acTL")
	binary.Write(&chunk, binary.BigEndian, uint32(2))
	binary.Write(&chunk, binary.BigEndian, uint32(0))
	binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(chunk.Bytes()[4:]))
	apng := append(append(append([]byte{}, base[:33]...), chunk.Bytes()...), base[33:]...)
	for _, test := range []struct {
		name string
		data []byte
		want error
	}{{"APNG", apng, imageproc.ErrUnsupported}, {"SVG", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), imageproc.ErrUnsupported}, {"corrupt GIF", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00bad"), imageproc.ErrInvalid}, {"truncated PNG", base[:len(base)/2], imageproc.ErrInvalid}} {
		t.Run(test.name, func(t *testing.T) {
			path := save(t, test.data)
			_, err := engine.Inspect(t.Context(), path)
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v want %v", err, test.want)
			}
			_, err = engine.Transform(t.Context(), path, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: "webp", Quality: 80, Distance: 1})
			if !errors.Is(err, test.want) {
				t.Fatalf("transform: %v", err)
			}
		})
	}
	vips.AssertNoLeaks(t)
}

func TestDimensionsAndResourceLimits(t *testing.T) {
	for _, test := range []struct {
		width, height int
		want          error
	}{{16383, 1, nil}, {16384, 1, imageproc.ErrDimensions}, {20000, 2000, imageproc.ErrDimensions}, {8000, 7000, imageproc.ErrDimensions}} {
		t.Run(fmt.Sprint(test.width, "x", test.height), func(t *testing.T) {
			// Header-only PNG: excessive dimensions must fail before pixel decode.
			data := testimage.PNG(1, 1, false)
			binary.BigEndian.PutUint32(data[16:20], uint32(test.width))
			binary.BigEndian.PutUint32(data[20:24], uint32(test.height))
			binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
			if test.want == nil {
				data = testimage.PNG(test.width, test.height, false)
			}
			_, err := engine.Inspect(t.Context(), save(t, data))
			if !errors.Is(err, test.want) {
				t.Fatalf("%v != %v", err, test.want)
			}
		})
	}
	limited := &Processor{limits: imageproc.Limits{MaxPixels: 100, MaxAxis: 16383, MaxFrames: 500, MaxOutputBytes: 10}}
	_, err := limited.Inspect(t.Context(), save(t, testimage.PNG(11, 10, false)))
	if !errors.Is(err, imageproc.ErrDimensions) {
		t.Fatal(err)
	}
	_, err = limited.Transform(t.Context(), save(t, testimage.PNG(5, 5, false)), filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: "webp", Quality: 80, Distance: 1})
	if !errors.Is(err, imageproc.ErrOutputLimit) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = engine.Inspect(ctx, "not-read")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	vips.AssertNoLeaks(t)
}

func TestRepeatedProcessingReleasesNativeResources(t *testing.T) {
	source := save(t, testimage.PNG(64, 48, true))
	for i := 0; i < 20; i++ {
		_, err := engine.Transform(t.Context(), source, filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: []string{"webp", "jxl"}[i%2], Quality: 80, Distance: 1})
		if err != nil {
			t.Fatal(err)
		}
		vips.AssertNoLeaks(t)
	}
}

func TestExtremeAspectRatios(t *testing.T) {
	for _, size := range [][2]int{{1, 1000}, {1000, 1}, {10, 10}} {
		for _, format := range []string{"webp", "jxl"} {
			meta, err := engine.Transform(t.Context(), save(t, testimage.PNG(size[0], size[1], false)), filepath.Join(t.TempDir(), "out"), imageproc.Options{Format: format, Width: 1, Height: 1, Quality: 80, Distance: 1})
			if err != nil {
				t.Fatal(err)
			}
			if meta.Width != 1 || meta.Height != 1 {
				t.Fatal(meta)
			}
		}
	}
	vips.AssertNoLeaks(t)
}

type cancellingWriter struct{ cancel context.CancelFunc }

func (w cancellingWriter) Write(data []byte) (int, error) { w.cancel(); return io.Discard.Write(data) }

func TestCancellationDuringNativeCallback(t *testing.T) {
	for _, data := range [][]byte{testimage.PNG(128, 96, true), testimage.GIF(128, 96, []int{4, 7, 13}, 0)} {
		ref, _, err := engine.load(t.Context(), save(t, data))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		err = evaluate(ctx, ref, func() error {
			return ref.SaveToWriterWebp(cancellingWriter{cancel: cancel}, vips.NewWebpExportParams())
		})
		cancel()
		ref.Close()
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		vips.AssertNoLeaks(t)
	}
}
