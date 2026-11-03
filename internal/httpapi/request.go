package httpapi

import (
	"io"
	"math"
	"mime"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

type Upload struct {
	reader io.Reader
	query  url.Values
}

func (u *Upload) Resolve(ctx huma.Context) []error {
	contentType, _, err := mime.ParseMediaType(ctx.Header("Content-Type"))
	if err != nil || contentType != "application/octet-stream" {
		return []error{problem(415, "unsupported-media-type", "Send image bytes as application/octet-stream.")}
	}
	u.reader = ctx.BodyReader()
	requestURL := ctx.URL()
	u.query, err = url.ParseQuery(requestURL.RawQuery)
	if err != nil {
		return []error{problem(400, "invalid-request", "The query string is malformed.")}
	}
	allowed := map[string]bool{}
	if ctx.Operation().OperationID == "transformImage" {
		for _, key := range []string{"format", "width", "height", "quality", "distance", "lossless"} {
			allowed[key] = true
		}
	}
	for key, values := range u.query {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			return []error{problem(400, "invalid-request", "Unknown, duplicate, or empty query parameter.")}
		}
	}
	return nil
}

type TransformInput struct {
	Upload
	Format   string  `query:"format" required:"true" enum:"webp,jxl"`
	Width    int     `query:"width" minimum:"1" maximum:"16383" doc:"Optional maximum output width; never upscale."`
	Height   int     `query:"height" minimum:"1" maximum:"16383" doc:"Optional maximum output height; never upscale."`
	Quality  int     `query:"quality" minimum:"0" maximum:"100" default:"80" doc:"Lossy WebP quality only."`
	Distance float64 `query:"distance" exclusiveMinimum:"0" maximum:"25" default:"1" doc:"Lossy JXL distance, greater than zero."`
	Lossless bool    `query:"lossless" default:"false"`
}

// options validates relationships that individual OpenAPI parameter schemas
// cannot express. Presence matters: an explicit quality is invalid for JXL,
// even when it happens to equal the WebP default.
func (input *TransformInput) options() (imageproc.Options, error) {
	wrongCodec := (input.Format == "webp" && input.query.Has("distance")) ||
		(input.Format == "jxl" && input.query.Has("quality"))
	lossyOptions := input.query.Has("quality") || input.query.Has("distance")
	invalidDistance := math.IsNaN(input.Distance) || math.IsInf(input.Distance, 0) ||
		(!input.Lossless && input.Distance <= 0)
	if wrongCodec || (input.Lossless && lossyOptions) || invalidDistance {
		return imageproc.Options{}, problem(400, "invalid-options", "Codec options must match the format and lossless mode.")
	}
	return imageproc.Options{
		Format:   input.Format,
		Width:    input.Width,
		Height:   input.Height,
		Quality:  input.Quality,
		Distance: input.Distance,
		Lossless: input.Lossless,
	}, nil
}
