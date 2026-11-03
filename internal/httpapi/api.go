package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/kilianlabs/image-transform/internal/buildinfo"
	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/service"
)

type Runner interface {
	Run(context.Context, io.Reader, *imageproc.Options) (*service.Result, error)
}
type App struct {
	Handler http.Handler
	api     huma.API
	runner  Runner
	ready   func() bool
}

func New(runner Runner, isReady func() bool) *App {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
		})
	})
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, problem(404, "not-found", "The requested endpoint does not exist."))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, problem(405, "method-not-allowed", "The method is not supported for this endpoint."))
	})
	config := huma.DefaultConfig("Image Transform", buildinfo.Version)
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	config.CreateHooks = nil
	config.Transformers = nil
	config.Info.Description = "Inspect JPEG, PNG, GIF, WebP, JXL, BMP, TIFF, HEIC/HEIF, and AVIF images and convert them to WebP or JXL on a trusted private network. GIF, WebP, and JXL animations are supported. TIFF uses the first page; HEIF/AVIF uses the primary image. Detected APNG and HEIF/AVIF image sequences are rejected. Native decoders may recover damaged inputs or omit unrecognized animation frames. Output colors target sRGB; reliable HDR tone mapping and NCLX-only color handling are not supported."
	api := humachi.New(router, config)
	app := &App{
		Handler: router,
		api:     api,
		runner:  runner,
		ready:   func() bool { return runner != nil && (isReady == nil || isReady()) },
	}
	app.registerOperations()
	// Ensure every error advertises the same named Problem and actual media type.
	problemSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[Problem](), true, "Problem")
	for _, path := range api.OpenAPI().Paths {
		for _, operation := range []*huma.Operation{path.Get, path.Post} {
			if operation == nil {
				continue
			}
			for code, response := range operation.Responses {
				if code != "200" {
					response.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: problemSchema}}
				}
			}
		}
	}
	router.Get("/api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		data, err := app.OpenAPI()
		if err != nil {
			writeProblem(w, problem(500, "internal", "OpenAPI generation failed."))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	return app
}

type Health struct {
	Status string `json:"status" enum:"ok"`
}

func (app *App) OpenAPI() ([]byte, error) {
	data, err := app.api.OpenAPI().Downgrade()
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := json.Indent(&output, data, "", "  "); err != nil {
		return nil, err
	}
	output.WriteByte('\n')
	return output.Bytes(), nil
}

// Timeouts apply to transport reads as well as the native processing context.
func Server(address string, handler http.Handler, timeout time.Duration) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: timeout, WriteTimeout: timeout + 5*time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
}
