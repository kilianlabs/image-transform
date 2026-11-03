package httpapi

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

func (app *App) registerOperations() {
	huma.Register(app.api, huma.Operation{Method: http.MethodGet, Path: "/health", OperationID: "getHealth", Summary: "Check native codec readiness", Errors: []int{503}}, func(ctx context.Context, _ *struct{}) (*struct{ Body Health }, error) {
		if !app.ready() {
			return nil, failure(imageproc.ErrUnavailable)
		}
		return &struct{ Body Health }{Body: Health{Status: "ok"}}, nil
	})
	operation := func(path, id, summary string) huma.Operation {
		return huma.Operation{Method: http.MethodPost, Path: path, OperationID: id, Summary: summary,
			RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}},
			Errors:      []int{400, 408, 413, 415, 422, 500, 503, 504}}
	}
	huma.Register(app.api, operation("/api/inspect", "inspectImage", "Validate an image and inspect its dimensions and animation metadata"), func(ctx context.Context, input *Upload) (*struct{ Body imageproc.Metadata }, error) {
		if !app.ready() {
			return nil, failure(imageproc.ErrUnavailable)
		}
		result, err := app.runner.Run(ctx, input.reader, nil)
		if err != nil {
			return nil, report(err, "inspect")
		}
		defer result.Close()
		return &struct{ Body imageproc.Metadata }{Body: result.Metadata}, nil
	})
	op := operation("/api/transform", "transformImage", "Generate one WebP or JXL derivative")
	op.Responses = map[string]*huma.Response{"200": {Description: "Validated still or animated image. Each frame fits within the requested bounds without upscaling or cropping.",
		Headers: map[string]*huma.Param{
			"X-Image-Width":       {Schema: &huma.Schema{Type: "integer", Minimum: new(float64(1))}},
			"X-Image-Height":      {Schema: &huma.Schema{Type: "integer", Minimum: new(float64(1))}},
			"X-Image-Frame-Count": {Schema: &huma.Schema{Type: "integer", Minimum: new(float64(1))}},
			"X-Image-SHA256":      {Schema: &huma.Schema{Type: "string", Pattern: "^[0-9a-f]{64}$"}},
			"Content-Length":      {Schema: &huma.Schema{Type: "integer"}},
		}, Content: map[string]*huma.MediaType{"image/webp": {Schema: &huma.Schema{Type: "string", Format: "binary"}}, "image/jxl": {Schema: &huma.Schema{Type: "string", Format: "binary"}}}}}
	huma.Register(app.api, op, func(ctx context.Context, input *TransformInput) (*huma.StreamResponse, error) {
		options, err := input.options()
		if err != nil {
			return nil, err
		}
		if !app.ready() {
			return nil, failure(imageproc.ErrUnavailable)
		}
		result, err := app.runner.Run(ctx, input.reader, &options)
		if err != nil {
			return nil, report(err, "transform")
		}
		file, err := os.Open(result.Path)
		if err != nil {
			result.Close()
			return nil, report(err, "read-output")
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer result.Close()
			defer file.Close()
			meta := result.Metadata
			hctx.SetHeader("Content-Type", meta.MIMEType)
			hctx.SetHeader("Content-Length", strconv.FormatInt(meta.ByteCount, 10))
			hctx.SetHeader("X-Image-Width", strconv.Itoa(meta.Width))
			hctx.SetHeader("X-Image-Height", strconv.Itoa(meta.Height))
			hctx.SetHeader("X-Image-Frame-Count", strconv.Itoa(meta.FrameCount))
			hctx.SetHeader("X-Image-SHA256", meta.SHA256)
			hctx.SetStatus(200)
			_, _ = io.Copy(hctx.BodyWriter(), file)
		}}, nil
	})
}
