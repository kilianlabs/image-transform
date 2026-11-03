package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kilianlabs/image-transform/internal/imageproc"
)

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (p *Problem) Error() string             { return p.Title }
func (p *Problem) GetStatus() int            { return p.Status }
func (p *Problem) ContentType(string) string { return "application/problem+json" }

// Huma's default validation errors echo values. Use the sidecar's sanitized
// error factory consistently, including framework-generated responses.
func init() {
	huma.NewError = func(status int, _ string, errs ...error) huma.StatusError {
		for _, err := range errs {
			var p *Problem
			if errors.As(err, &p) {
				return p
			}
		}
		if status == 422 {
			status = 400
		}
		return problem(status, "invalid-request", "The request does not match the OpenAPI contract.")
	}
}

func problem(status int, code, detail string) *Problem {
	return &Problem{Type: "urn:image-transform:error:" + code, Title: http.StatusText(status), Status: status, Detail: detail}
}
func failure(err error) *Problem {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return problem(504, "timeout", "Image processing exceeded the request deadline.")
	case errors.Is(err, context.Canceled):
		return problem(503, "cancelled", "Image processing was cancelled.")
	case errors.Is(err, os.ErrDeadlineExceeded):
		return problem(408, "request-timeout", "The request body was not received before its deadline.")
	case errors.Is(err, imageproc.ErrTooLarge):
		return problem(413, "payload-too-large", "The image exceeds the configured input byte limit.")
	case errors.Is(err, imageproc.ErrUnsupported):
		return problem(415, "unsupported-format", "Supported inputs are JPEG, PNG, GIF, WebP, JXL, BMP, TIFF, HEIC/HEIF, and AVIF. APNG and HEIF/AVIF image sequences are not supported.")
	case errors.Is(err, imageproc.ErrFrames):
		return problem(422, "frame-limit", "The image exceeds the configured frame limit.")
	case errors.Is(err, imageproc.ErrDimensions):
		return problem(422, "image-dimensions", "The image exceeds the configured total pixel or per-frame axis limit.")
	case errors.Is(err, imageproc.ErrInvalid):
		return problem(422, "invalid-image", "The image could not be decoded and processed.")
	case errors.Is(err, imageproc.ErrBusy):
		return problem(503, "busy", "All image processing slots are occupied.")
	case errors.Is(err, imageproc.ErrUnavailable):
		return problem(503, "unavailable", "Image processing is not ready.")
	case errors.Is(err, imageproc.ErrOutputLimit):
		return problem(422, "output-limit", "The derivative exceeds the configured output byte limit.")
	default:
		return problem(500, "internal", "Image processing failed.")
	}
}
func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func report(err error, operation string) *Problem {
	p := failure(err)
	slog.Warn("Image request failed", "operation", operation, "error_type", p.Type, "status", p.Status)
	return p
}
