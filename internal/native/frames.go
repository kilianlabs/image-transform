package native

import (
	"context"
	"math"
	"slices"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

func (p *Processor) load(ctx context.Context, path string) (*vips.ImageRef, imageproc.Metadata, error) {
	var info imageproc.Metadata
	if err := ctx.Err(); err != nil {
		return nil, info, err
	}
	mime, err := detectFormat(ctx, path)
	if err != nil {
		return nil, info, err
	}
	// Load only the first frame to read geometry and animation headers before
	// constructing the full decoded strip. The service bounds compressed bytes.
	ref, err := vips.LoadImageFromFile(path, vips.NewImportParams())
	if err != nil {
		return nil, info, inputError(ctx)
	}
	fail := func(err error) (*vips.ImageRef, imageproc.Metadata, error) {
		ref.Close()
		return nil, info, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	info.MIMEType = mime
	info.Width, info.Height, info.FrameCount = ref.Width(), ref.PageHeight(), ref.Pages()
	// TIFF pages and HEIF top-level images are collections, not animation
	// frames. Defaults select TIFF page zero and HEIF's primary image.
	if mime == "image/tiff" || mime == "image/heic" || mime == "image/heif" || mime == "image/avif" {
		page := 0
		if mime != "image/tiff" {
			page = ref.GetInt("heif-primary")
		}
		info.PageCount, info.PageIndex = info.FrameCount, &page
		info.FrameCount = 1
		if info.PageCount < 1 || page < 0 || page >= info.PageCount {
			return fail(imageproc.ErrInvalid)
		}
		if err := ref.SetPages(1); err != nil {
			return fail(err)
		}
	}
	if err := p.limits.Check(info.Width, info.Height, info.FrameCount); err != nil {
		return fail(err)
	}
	info.OrientedWidth, info.OrientedHeight = info.Width, info.Height
	if orientation := ref.Orientation(); orientation >= 5 && orientation <= 8 {
		info.OrientedWidth, info.OrientedHeight = info.Height, info.Width
	}
	if info.FrameCount == 1 {
		return ref, info, nil
	}
	// Untimed multi-page files must not be turned into invented animations.
	if !slices.Contains(ref.GetFields(), "delay") {
		return fail(imageproc.ErrUnsupported)
	}
	info.FrameDelaysMS, err = ref.PageDelay()
	if err != nil || len(info.FrameDelaysMS) != info.FrameCount {
		return fail(imageproc.ErrInvalid)
	}
	for _, delay := range info.FrameDelaysMS {
		if delay < 0 {
			return fail(imageproc.ErrInvalid)
		}
	}
	loop := ref.Loop()
	if loop < 0 {
		return fail(imageproc.ErrInvalid)
	}
	info.LoopCount = &loop
	ref.Close()
	params := vips.NewImportParams()
	params.NumPages.Set(-1)
	width, height := info.Width, info.Height
	if info.MIMEType == "image/jxl" {
		// govips v2.20's regular JXL loader ignores NumPages. Its generic
		// thumbnail loader passes n=-1 to libvips correctly. A full-size square
		// bound prevents resizing; this loader also applies orientation/color.
		bound := max(info.OrientedWidth, info.OrientedHeight)
		ref, err = vips.LoadThumbnailFromFile(path, bound, bound, vips.InterestingNone, vips.SizeDown, params)
		width, height = info.OrientedWidth, info.OrientedHeight
	} else {
		ref, err = vips.LoadImageFromFile(path, params)
	}
	if err != nil {
		return nil, info, inputError(ctx)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if ref.Width() != width || ref.PageHeight() != height || ref.Height() != height*info.FrameCount || ref.Pages() != info.FrameCount {
		return fail(imageproc.ErrInvalid)
	}
	return ref, info, nil
}

func prepareFrames(ctx context.Context, source *vips.ImageRef, info imageproc.Metadata, options imageproc.Options) (*vips.ImageRef, error) {
	frames := make([]*vips.ImageRef, 0, info.FrameCount)
	keepFirst := false
	defer func() {
		for i, frame := range frames {
			if i != 0 || !keepFirst {
				frame.Close()
			}
		}
	}()
	for i := 0; i < info.FrameCount; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := source.Copy()
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
		if info.FrameCount > 1 {
			// Extract one frame from the strip. govips otherwise interprets
			// ExtractArea as a crop applied to every page.
			if err := frame.SetPageHeight(source.Height()); err != nil {
				return nil, err
			}
			if err := frame.SetPages(1); err != nil {
				return nil, err
			}
			if err := frame.ExtractArea(0, i*source.PageHeight(), source.Width(), source.PageHeight()); err != nil {
				return nil, err
			}
		}
		if err := normalizeFrame(ctx, frame, options); err != nil {
			return nil, err
		}
	}
	result := frames[0]
	height := result.Height()
	if len(frames) > 1 {
		if err := result.ArrayJoin(frames[1:], 1); err != nil {
			return nil, err
		}
		if err := result.SetPageDelay(info.FrameDelaysMS); err != nil {
			return nil, err
		}
		if err := result.SetLoop(*info.LoopCount); err != nil {
			return nil, err
		}
	}
	if err := result.SetPageHeight(height); err != nil {
		return nil, err
	}
	if err := result.SetPages(len(frames)); err != nil {
		return nil, err
	}
	keepFirst = true
	return result, nil
}

func normalizeFrame(ctx context.Context, frame *vips.ImageRef, options imageproc.Options) error {
	steps := []func() error{frame.AutoRotate, func() error {
		if frame.HasICCProfile() {
			return frame.TransformICCProfile("srgb")
		}
		return frame.ToColorSpace(vips.InterpretationSRGB)
	}}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step(); err != nil {
			return err
		}
	}
	scale := 1.0
	if options.Width > 0 {
		scale = math.Min(scale, float64(options.Width)/float64(frame.Width()))
	}
	if options.Height > 0 {
		scale = math.Min(scale, float64(options.Height)/float64(frame.Height()))
	}
	// Resize each frame separately: filtering a stacked strip mixes pixels
	// from adjacent frames and rounds its total height instead of each frame.
	if scale < 1 {
		if err := frame.SetPageHeight(frame.Height()); err != nil {
			return err
		}
		if err := frame.Resize(scale, vips.KernelLanczos3); err != nil {
			return err
		}
	}
	return frame.RemoveMetadata("icc-profile-data")
}

func outputDelays(delays []int) []int {
	result := slices.Clone(delays)
	for i, delay := range result {
		// Both libvips encoders use this browser-compatible minimum.
		if delay <= 10 {
			result[i] = 100
		}
	}
	return result
}

func animationMatches(input, output imageproc.Metadata) bool {
	if input.FrameCount == 1 {
		return output.FrameCount == 1
	}
	// WebP can coalesce identical frames, including an entirely static result.
	if output.MIMEType == "image/webp" && output.FrameCount == 1 {
		return true
	}
	if output.FrameCount > input.FrameCount || output.LoopCount == nil || *output.LoopCount != *input.LoopCount {
		return false
	}
	expected := outputDelays(input.FrameDelaysMS)
	if output.MIMEType == "image/jxl" {
		return slices.Equal(expected, output.FrameDelaysMS)
	}
	var before, after int64
	for _, delay := range expected {
		before += int64(delay)
	}
	for _, delay := range output.FrameDelaysMS {
		after += int64(delay)
	}
	return before == after
}
