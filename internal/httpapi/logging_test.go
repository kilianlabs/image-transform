package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kilianlabs/image-transform/internal/imageproc"
	"github.com/kilianlabs/image-transform/internal/service"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)).With("service", "image-transform"))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &output
}

func readLogs(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	decoder := json.NewDecoder(output)
	for {
		var entry map[string]any
		if err := decoder.Decode(&entry); errors.Is(err, io.EOF) {
			return entries
		} else if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
}

type missingOutputRunner struct{ Runner }

func (runner missingOutputRunner) Run(ctx context.Context, body io.Reader, options *imageproc.Options) (*service.Result, error) {
	result, err := runner.Runner.Run(ctx, body, options)
	if err == nil {
		result.Path += ".missing"
	}
	return result, err
}

func TestRequestLogsCorrelateOperationFailures(t *testing.T) {
	generated := map[string]bool{}
	for _, suppliedID := range []string{"upstream-request-123", ""} {
		for _, operation := range []string{"inspect", "transform", "read-output"} {
			t.Run(operation+"/"+suppliedID, func(t *testing.T) {
				output := captureLogs(t)
				processor := &fakeProcessor{err: errors.New("secret-codec-error")}
				app, _ := setup(t, processor)
				path := "/api/inspect"
				if operation != "inspect" {
					path = "/api/transform?format=webp"
				}
				if operation == "read-output" {
					processor.err = nil
					app.runner = missingOutputRunner{app.runner}
				}
				request := httptest.NewRequest("POST", path, strings.NewReader("secret-body"))
				request.Header.Set("Content-Type", "application/octet-stream")
				if suppliedID != "" {
					request.Header.Set("X-Request-ID", suppliedID)
				}
				response := httptest.NewRecorder()
				app.Handler.ServeHTTP(response, request)
				if response.Code != 500 {
					t.Fatalf("status = %d, want 500", response.Code)
				}
				if strings.Contains(output.String(), "secret-") {
					t.Fatalf("sensitive data logged: %s", output)
				}
				entries := readLogs(t, output)
				if len(entries) != 2 {
					t.Fatalf("want operation and access logs, got %v", entries)
				}
				failure, access := entries[0], entries[1]
				requestID, _ := access["request_id"].(string)
				if requestID == "" || failure["request_id"] != requestID {
					t.Fatalf("uncorrelated logs: %v", entries)
				}
				if suppliedID != "" && requestID != suppliedID {
					t.Fatalf("request ID = %q, want %q", requestID, suppliedID)
				}
				if suppliedID == "" {
					if generated[requestID] || requestID == "upstream-request-123" {
						t.Fatalf("request ID reused: %q", requestID)
					}
					generated[requestID] = true
				}
				if failure["operation"] != operation || failure["error_type"] != "urn:image-transform:error:internal" || failure["status"] != float64(500) {
					t.Fatalf("unexpected operation log: %v", failure)
				}
				if access["event"] != "http_request" || access["status"] != float64(500) {
					t.Fatalf("unexpected access log: %v", access)
				}
				for _, entry := range entries {
					if entry["service"] != "image-transform" {
						t.Fatalf("missing service: %v", entry)
					}
				}
			})
		}
	}
}

func TestAccessLogsCoverAllRoutes(t *testing.T) {
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/health", 200},
		{"GET", "/api/openapi.json", 200},
		{"POST", "/api/inspect", 200},
		{"POST", "/api/transform?format=webp", 200},
		{"POST", "/api/transform?token=secret-query", 400},
		{"GET", "/missing?token=secret-query", 404},
		{"GET", "/api/inspect", 405},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			output := captureLogs(t)
			app, _ := setup(t, &fakeProcessor{})
			request := httptest.NewRequest(test.method, test.path, strings.NewReader("secret-body"))
			request.Header.Set("Content-Type", "application/octet-stream")
			request.Header.Set("Authorization", "Bearer secret-token")
			request.Header.Set("Cookie", "session=secret-cookie")
			request.Header.Set("X-Request-ID", "")
			response := httptest.NewRecorder()
			app.Handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if strings.Contains(output.String(), "secret-") {
				t.Fatalf("sensitive request data logged: %s", output)
			}
			entries := readLogs(t, output)
			if len(entries) != 1 {
				t.Fatalf("want one access log, got %v", entries)
			}
			entry := entries[0]
			if entry["event"] != "http_request" || entry["method"] != test.method || entry["path"] != request.URL.Path || entry["status"] != float64(test.status) || entry["proto"] != request.Proto || entry["size"] != float64(response.Body.Len()) {
				t.Fatalf("unexpected access log: %v", entry)
			}
			if id, _ := entry["request_id"].(string); id == "" {
				t.Fatalf("missing request ID: %v", entry)
			}
			if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 {
				t.Fatalf("invalid duration: %v", entry)
			}
		})
	}
}
