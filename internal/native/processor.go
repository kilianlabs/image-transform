// Package native is the only production package that imports govips.
package native

import (
	"context"
	"errors"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

type Processor struct{ limits imageproc.Limits }

func Start(limits imageproc.Limits, threads int) (*Processor, error) {
	// Codec exceptions can contain metadata, paths, or image contents.
	vips.LoggingSettings(func(string, vips.LogLevel, string) {}, vips.LogLevelError)
	if err := vips.Startup(&vips.Config{ConcurrencyLevel: threads, MaxCacheMem: 32 << 20, MaxCacheFiles: 0, MaxCacheSize: 0}); err != nil {
		return nil, errors.New("libvips initialization failed")
	}
	return &Processor{limits: limits}, nil
}

func Shutdown() { vips.Shutdown() }

func (p *Processor) Inspect(ctx context.Context, path string) (imageproc.Metadata, error) {
	ref, info, err := p.load(ctx, path)
	if err != nil {
		return info, err
	}
	defer ref.Close()
	// Evaluate pixels as well as headers. Native decoders may recover damaged inputs.
	err = evaluate(ctx, ref, func() error { _, err := ref.Average(); return err })
	if err != nil {
		return info, inputError(ctx)
	}
	return info, nil
}

func (p *Processor) Transform(ctx context.Context, source, destination string, options imageproc.Options) (imageproc.Metadata, error) {
	sourceImage, info, err := p.load(ctx, source)
	if err != nil {
		return info, err
	}
	defer sourceImage.Close()
	ref, err := prepareFrames(ctx, sourceImage, info, options)
	if err != nil {
		return info, inputError(ctx)
	}
	defer ref.Close()
	width, height := ref.Width(), ref.PageHeight()
	if err := p.encode(ctx, ref, destination, options); err != nil {
		return info, err
	}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	decoded, result, err := p.load(ctx, destination)
	if err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, errors.New("output validation failed")
	}
	defer decoded.Close()
	if result.Width != width || result.Height != height || result.MIMEType != "image/"+options.Format || !animationMatches(info, result) {
		return info, errors.New("output validation failed")
	}
	err = evaluate(ctx, decoded, func() error { _, err := decoded.Average(); return err })
	if err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, errors.New("output validation failed")
	}
	return result, nil
}

func evaluate(ctx context.Context, ref *vips.ImageRef, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { ref.SetKill(true); close(finished) })
	err := fn()
	if !stop() {
		<-finished
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func inputError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return imageproc.ErrInvalid
}
