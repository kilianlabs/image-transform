package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Host                   string
	Port                   int
	TempDir                string
	MaxInputBytes          int64
	MaxOutputBytes         int64
	MaxPixels              int64
	MaxAxis                int
	MaxFrames              int
	MaxConcurrent          int
	MaxInspectConcurrent   int
	MaxTransformConcurrent int
	VIPSThreads            int
	RequestTimeout         time.Duration
}

func Read(getenv func(string) string) (Config, error) {
	c := Config{
		Host:           "127.0.0.1",
		Port:           3000,
		TempDir:        os.TempDir(),
		MaxInputBytes:  100 << 20,
		MaxOutputBytes: 256 << 20,
		MaxPixels:      50_000_000,
		MaxAxis:        16_383,
		MaxFrames:      500,
		MaxConcurrent:  1,
		VIPSThreads:    2,
		RequestTimeout: 120 * time.Second,
	}
	if v := getenv("HOST"); v != "" {
		c.Host = v
	}
	if v := getenv("TMPDIR"); v != "" {
		c.TempDir = v
	}
	for _, item := range []struct {
		name string
		dst  *int64
		max  int64
	}{
		{"MAX_INPUT_BYTES", &c.MaxInputBytes, 1 << 40},
		{"MAX_OUTPUT_BYTES", &c.MaxOutputBytes, 1 << 40},
		{"MAX_PIXELS", &c.MaxPixels, 16_383 * 16_383},
	} {
		if v := getenv(item.name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 || n > item.max {
				return c, fmt.Errorf("invalid %s", item.name)
			}
			*item.dst = n
		}
	}
	for _, item := range []struct {
		name string
		dst  *int
		max  int
	}{
		{"PORT", &c.Port, 65535}, {"MAX_AXIS", &c.MaxAxis, 16_383},
		{"MAX_FRAMES", &c.MaxFrames, 10000},
		{"MAX_CONCURRENT", &c.MaxConcurrent, 64}, {"VIPS_THREADS", &c.VIPSThreads, 64},
	} {
		if v := getenv(item.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > item.max {
				return c, fmt.Errorf("invalid %s", item.name)
			}
			*item.dst = n
		}
	}
	if v := getenv("REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 || d > time.Hour {
			return c, fmt.Errorf("invalid REQUEST_TIMEOUT")
		}
		c.RequestTimeout = d
	}
	c.MaxInspectConcurrent, c.MaxTransformConcurrent = c.MaxConcurrent, c.MaxConcurrent
	for _, item := range []struct {
		name string
		dst  *int
		max  int
	}{
		{"MAX_INSPECT_CONCURRENT", &c.MaxInspectConcurrent, c.MaxConcurrent},
		{"MAX_TRANSFORM_CONCURRENT", &c.MaxTransformConcurrent, c.MaxConcurrent},
	} {
		if v := getenv(item.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > item.max {
				return c, fmt.Errorf("invalid %s", item.name)
			}
			*item.dst = n
		}
	}
	return c, nil
}
