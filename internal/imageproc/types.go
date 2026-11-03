// Package imageproc defines the native-library-independent processing boundary.
package imageproc

import (
	"context"
	"errors"
)

var (
	ErrUnsupported = errors.New("unsupported-format")
	ErrFrames      = errors.New("frame-limit")
	ErrInvalid     = errors.New("invalid-image")
	ErrDimensions  = errors.New("image-dimensions")
	ErrTooLarge    = errors.New("payload-too-large")
	ErrBusy        = errors.New("busy")
	ErrUnavailable = errors.New("unavailable")
	ErrOutputLimit = errors.New("output-limit")
)

type Metadata struct {
	MIMEType       string `json:"mime_type"`
	ByteCount      int64  `json:"byte_count"`
	SHA256         string `json:"sha256" pattern:"^[0-9a-f]{64}$"`
	Width          int    `json:"width" minimum:"1"`
	Height         int    `json:"height" minimum:"1"`
	OrientedWidth  int    `json:"oriented_width" minimum:"1"`
	OrientedHeight int    `json:"oriented_height" minimum:"1"`
	FrameCount     int    `json:"frame_count" minimum:"1" doc:"Number of frames; 1 for a still image."`
	FrameDelaysMS  []int  `json:"frame_delays_ms,omitempty" doc:"Frame durations in milliseconds, for animated images."`
	LoopCount      *int   `json:"loop_count,omitempty" minimum:"0" doc:"Animation plays; 0 means repeat indefinitely."`
	PageCount      int    `json:"page_count,omitempty" minimum:"1" doc:"Number of top-level images in a TIFF or HEIF/AVIF collection."`
	PageIndex      *int   `json:"page_index,omitempty" minimum:"0" doc:"Selected image in a collection, numbered from zero. TIFF uses the first page; HEIF/AVIF uses the primary image."`
}

type Options struct {
	Format        string
	Width, Height int
	Quality       int
	Distance      float64
	Lossless      bool
}

type Limits struct {
	MaxPixels      int64
	MaxAxis        int
	MaxFrames      int
	MaxOutputBytes int64
}

// Check applies source limits to the entire animation, before resizing.
func (l Limits) Check(width, height, frames int) error {
	if frames < 1 || width < 1 || height < 1 {
		return ErrInvalid
	}
	if frames > l.MaxFrames {
		return ErrFrames
	}
	if width > l.MaxAxis || height > l.MaxAxis || int64(width)*int64(height)*int64(frames) > l.MaxPixels {
		return ErrDimensions
	}
	return nil
}

type ImageProcessor interface {
	Inspect(context.Context, string) (Metadata, error)
	Transform(context.Context, string, string, Options) (Metadata, error)
}
