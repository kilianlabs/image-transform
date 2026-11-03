package native

import (
	"context"
	"io"
	"os"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

func (p *Processor) encode(ctx context.Context, ref *vips.ImageRef, destination string, options imageproc.Options) error {
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	writer := &limitedWriter{writer: out, remaining: p.limits.MaxOutputBytes}
	err = evaluate(ctx, ref, func() error {
		switch options.Format {
		case "webp":
			params := vips.NewWebpExportParams()
			params.Quality = options.Quality
			params.Lossless = options.Lossless
			params.Keep = vips.ForeignKeepIcc
			return ref.SaveToWriterWebp(writer, params)
		case "jxl":
			params := vips.NewJxlExportParams()
			params.Distance = options.Distance
			params.Lossless = options.Lossless
			params.Keep = vips.ForeignKeepIcc
			if options.Lossless {
				params.Distance = 0
			}
			// govips exposes JXL encoding through its buffer API. Native and Go
			// output allocations are contained by the container memory limit.
			data, _, err := ref.ExportJxl(params)
			if err != nil {
				return err
			}
			_, err = writer.Write(data)
			return err
		default:
			return imageproc.ErrUnsupported
		}
	})
	closeErr := out.Close()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if writer.exceeded {
			return imageproc.ErrOutputLimit
		}
		if writer.err != nil {
			return writer.err
		}
		return inputError(ctx)
	}
	return closeErr
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
	exceeded  bool
	err       error
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		w.exceeded = true
		return 0, imageproc.ErrOutputLimit
	}
	n, err := w.writer.Write(p)
	w.err = err
	w.remaining -= int64(n)
	return n, err
}
