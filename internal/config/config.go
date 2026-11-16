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
	if err := readIntegers(getenv, []integerSetting[int64]{
		{"MAX_INPUT_BYTES", &c.MaxInputBytes, 1 << 40},
		{"MAX_OUTPUT_BYTES", &c.MaxOutputBytes, 1 << 40},
		{"MAX_PIXELS", &c.MaxPixels, 16_383 * 16_383},
	}); err != nil {
		return c, err
	}
	if err := readIntegers(getenv, []integerSetting[int]{
		{"PORT", &c.Port, 65535}, {"MAX_AXIS", &c.MaxAxis, 16_383},
		{"MAX_FRAMES", &c.MaxFrames, 10000},
		{"MAX_CONCURRENT", &c.MaxConcurrent, 64}, {"VIPS_THREADS", &c.VIPSThreads, 64},
	}); err != nil {
		return c, err
	}
	if v := getenv("REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 || d > time.Hour {
			return c, fmt.Errorf("invalid REQUEST_TIMEOUT")
		}
		c.RequestTimeout = d
	}
	c.MaxInspectConcurrent, c.MaxTransformConcurrent = c.MaxConcurrent, c.MaxConcurrent
	if err := readIntegers(getenv, []integerSetting[int]{
		{"MAX_INSPECT_CONCURRENT", &c.MaxInspectConcurrent, c.MaxConcurrent},
		{"MAX_TRANSFORM_CONCURRENT", &c.MaxTransformConcurrent, c.MaxConcurrent},
	}); err != nil {
		return c, err
	}
	return c, nil
}

type integerSetting[T int | int64] struct {
	name string
	dst  *T
	max  T
}

func readIntegers[T int | int64](getenv func(string) string, settings []integerSetting[T]) error {
	for _, setting := range settings {
		value := getenv(setting.name)
		if value == "" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || n > int64(setting.max) {
			return fmt.Errorf("invalid %s", setting.name)
		}
		*setting.dst = T(n)
	}
	return nil
}
