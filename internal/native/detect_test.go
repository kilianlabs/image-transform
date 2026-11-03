package native

import "testing"

func TestBigTIFFDetection(t *testing.T) {
	for _, header := range []string{
		"II\x2b\x00\x08\x00\x00\x00\x10\x00\x00\x00\x00\x00\x00\x00",
		"MM\x00\x2b\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x10",
	} {
		mime, err := detectFormat(t.Context(), save(t, []byte(header)))
		if err != nil || mime != "image/tiff" {
			t.Fatalf("%s %v", mime, err)
		}
	}
}
