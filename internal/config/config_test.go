package config

import "testing"

func TestConfiguration(t *testing.T) {
	c, err := Read(func(string) string { return "" })
	if err != nil || c.MaxAxis != 16383 || c.MaxPixels != 50_000_000 || c.Port != 3000 || c.MaxFrames != 500 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for name, value := range map[string]string{"PORT": "65536", "MAX_AXIS": "16384", "MAX_PIXELS": "0", "MAX_FRAMES": "10001", "VIPS_THREADS": "-1", "MAX_CONCURRENT": "many", "MAX_INPUT_BYTES": "-1", "REQUEST_TIMEOUT": "0s"} {
		t.Run(name, func(t *testing.T) {
			_, err := Read(func(key string) string {
				if key == name {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
}

func TestAdmissionConfiguration(t *testing.T) {
	c, err := Read(func(key string) string {
		if key == "MAX_CONCURRENT" {
			return "3"
		}
		return ""
	})
	if err != nil || c.MaxInspectConcurrent != 3 || c.MaxTransformConcurrent != 3 {
		t.Fatal(c, err)
	}
	values := map[string]string{"MAX_CONCURRENT": "2", "MAX_INSPECT_CONCURRENT": "1", "MAX_TRANSFORM_CONCURRENT": "1"}
	c, err = Read(func(key string) string { return values[key] })
	if err != nil || c.MaxInspectConcurrent != 1 || c.MaxTransformConcurrent != 1 {
		t.Fatal(c, err)
	}
	for name, value := range map[string]string{"MAX_INSPECT_CONCURRENT": "3", "MAX_TRANSFORM_CONCURRENT": "0"} {
		t.Run(name, func(t *testing.T) {
			_, err := Read(func(key string) string {
				if key == name {
					return value
				}
				return values[key]
			})
			if err == nil {
				t.Fatal("invalid limit accepted")
			}
		})
	}
}
