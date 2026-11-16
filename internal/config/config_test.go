package config

import (
	"strconv"
	"testing"
	"time"
)

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

func TestIntegerSettingBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		max  int64
		get  func(Config) int64
	}{
		{"MAX_INPUT_BYTES", 1 << 40, func(c Config) int64 { return c.MaxInputBytes }},
		{"MAX_OUTPUT_BYTES", 1 << 40, func(c Config) int64 { return c.MaxOutputBytes }},
		{"MAX_PIXELS", 16_383 * 16_383, func(c Config) int64 { return c.MaxPixels }},
		{"PORT", 65535, func(c Config) int64 { return int64(c.Port) }},
		{"MAX_AXIS", 16_383, func(c Config) int64 { return int64(c.MaxAxis) }},
		{"MAX_FRAMES", 10000, func(c Config) int64 { return int64(c.MaxFrames) }},
		{"MAX_CONCURRENT", 64, func(c Config) int64 { return int64(c.MaxConcurrent) }},
		{"VIPS_THREADS", 64, func(c Config) int64 { return int64(c.VIPSThreads) }},
		{"MAX_INSPECT_CONCURRENT", 64, func(c Config) int64 { return int64(c.MaxInspectConcurrent) }},
		{"MAX_TRANSFORM_CONCURRENT", 64, func(c Config) int64 { return int64(c.MaxTransformConcurrent) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range []string{"1", strconv.FormatInt(test.max, 10), "0", "-1", strconv.FormatInt(test.max+1, 10), "9223372036854775808", "1.5", "many"} {
				t.Run(value, func(t *testing.T) {
					c, err := Read(func(key string) string {
						if key == test.name {
							return value
						}
						if key == "MAX_CONCURRENT" {
							return "64"
						}
						return ""
					})
					if value == "1" || value == strconv.FormatInt(test.max, 10) {
						if err != nil || strconv.FormatInt(test.get(c), 10) != value {
							t.Fatalf("setting not applied: %+v %v", c, err)
						}
					} else if err == nil || err.Error() != "invalid "+test.name {
						t.Fatalf("expected invalid %s, got %v", test.name, err)
					}
				})
			}
		})
	}
}

func TestNonIntegerSettings(t *testing.T) {
	values := map[string]string{"HOST": "::1", "TMPDIR": t.TempDir(), "REQUEST_TIMEOUT": "1h"}
	c, err := Read(func(key string) string { return values[key] })
	if err != nil || c.Host != "::1" || c.TempDir != values["TMPDIR"] || c.RequestTimeout != time.Hour {
		t.Fatalf("settings not applied: %+v %v", c, err)
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
