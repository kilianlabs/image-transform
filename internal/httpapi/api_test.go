package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/service"
)

type fakeProcessor struct {
	err     error
	calls   int
	options imageproc.Options
}

func (p *fakeProcessor) Inspect(context.Context, string) (imageproc.Metadata, error) {
	p.calls++
	return imageproc.Metadata{MIMEType: "image/png", Width: 10, Height: 5, OrientedWidth: 10, OrientedHeight: 5, FrameCount: 1}, p.err
}
func (p *fakeProcessor) Transform(_ context.Context, _, dst string, options imageproc.Options) (imageproc.Metadata, error) {
	p.calls++
	p.options = options
	if p.err != nil {
		return imageproc.Metadata{}, p.err
	}
	err := os.WriteFile(dst, []byte("encoded"), 0600)
	return imageproc.Metadata{MIMEType: "image/" + options.Format, Width: 5, Height: 3, OrientedWidth: 5, OrientedHeight: 3, FrameCount: 1}, err
}

func setup(t *testing.T, p *fakeProcessor) (*App, *service.Service) {
	t.Helper()
	s := service.New(p, t.TempDir(), 100, service.AdmissionLimits{Concurrent: 1}, time.Second)
	t.Cleanup(func() { s.Stop(); s.Wait() })
	return New(s, nil), s
}

func TestBusyContractAndHealthUnderSaturation(t *testing.T) {
	app, service := setup(t, new(fakeProcessor))
	result, err := service.Run(t.Context(), strings.NewReader("source"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	w := request(app, "/api/inspect", "source", "application/octet-stream")
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if w.Code != 503 || p.Type != "urn:image-transform:error:busy" {
		t.Fatal(w.Code, w.Header(), p)
	}
	health := httptest.NewRecorder()
	app.Handler.ServeHTTP(health, httptest.NewRequest("GET", "/health", nil))
	if health.Code != 200 {
		t.Fatal("health depends on image capacity", health.Code)
	}
}
func request(app *App, path, body, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	app.Handler.ServeHTTP(w, r)
	return w
}

func TestImageContract(t *testing.T) {
	p := &fakeProcessor{}
	app, _ := setup(t, p)
	w := request(app, "/api/inspect", "source", "application/octet-stream; charset=binary")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var result imageproc.Metadata
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ByteCount != 6 || len(result.SHA256) != 64 || result.OrientedWidth != 10 || result.FrameCount != 1 {
		t.Fatal(result)
	}
	w = request(app, "/api/transform?format=webp&width=5", "source", "application/octet-stream")
	if w.Code != 200 || w.Body.String() != "encoded" || w.Header().Get("Content-Type") != "image/webp" || w.Header().Get("X-Image-Width") != "5" || w.Header().Get("Content-Length") != "7" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
	}
	if p.options.Quality != 80 || p.options.Distance != 1 || p.options.Width != 5 {
		t.Fatal(p.options)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Image-Frame-Count") != "1" {
		t.Fatal("cacheable response")
	}
}

func TestInvalidRequestsDoNotProcess(t *testing.T) {
	for _, query := range []string{"", "format=gif", "format=webp&width=0", "format=webp&height=-1", "format=webp&width=16384", "format=jxl&distance=NaN", "format=jxl&distance=0", "format=webp&quality=101", "format=webp&distance=1", "format=jxl&quality=80", "format=jxl&lossless=true&distance=1", "format=webp&format=jxl", "format=webp&wat=1", "format=webp&height=", "format=webp&lossless=maybe", "format=webp&width=%ZZ"} {
		t.Run(query, func(t *testing.T) {
			p := &fakeProcessor{}
			app, _ := setup(t, p)
			w := request(app, "/api/transform?"+query, "source", "application/octet-stream")
			if w.Code != 400 || p.calls != 0 {
				t.Fatalf("%d calls=%d %s", w.Code, p.calls, w.Body)
			}
		})
	}
}

func TestProblemResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{{"frames", imageproc.ErrFrames, 422}, {"invalid", imageproc.ErrInvalid, 422}, {"dimensions", imageproc.ErrDimensions, 422}, {"unsupported", imageproc.ErrUnsupported, 415}, {"internal", errors.New("secret metadata"), 500}} {
		t.Run(test.name, func(t *testing.T) {
			app, _ := setup(t, &fakeProcessor{err: test.err})
			w := request(app, "/api/inspect", "source", "application/octet-stream")
			if w.Code != test.status || !strings.Contains(w.Header().Get("Content-Type"), "application/problem+json") || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var p Problem
			json.Unmarshal(w.Body.Bytes(), &p)
			if p.Status != test.status {
				t.Fatal(p)
			}
		})
	}
	app, _ := setup(t, &fakeProcessor{})
	if w := request(app, "/api/inspect", "source", "image/png"); w.Code != 415 || !strings.Contains(w.Body.String(), "urn:image-transform:error:unsupported-media-type") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := request(app, "/api/inspect", strings.Repeat("x", 101), "application/octet-stream"); w.Code != 413 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestOpenAPIAndReadiness(t *testing.T) {
	app := New(nil, nil)
	for _, test := range []struct {
		path   string
		status int
	}{{"/health", 503}, {"/api/openapi.json", 200}, {"/docs", 404}, {"/schemas/Metadata.json", 404}, {"/v1/inspect", 404}} {
		w := httptest.NewRecorder()
		app.Handler.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
		if w.Code != test.status {
			t.Fatalf("%s: %d", test.path, w.Code)
		}
	}
	data, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string                     `json:"operationId"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.0.3" || doc.Paths["/api/transform"]["post"].OperationID != "transformImage" {
		t.Fatal("invalid contract")
	}
	for _, status := range []string{"400", "408", "413", "415", "422", "500", "503", "504"} {
		if _, ok := doc.Paths["/api/transform"]["post"].Responses[status]; !ok {
			t.Fatal("missing status", status)
		}
	}
	if strings.Contains(string(data), "application/json\": {\n            \"schema\": {\n              \"format\": \"binary\"") {
		t.Fatal("binary documented as JSON")
	}
}
