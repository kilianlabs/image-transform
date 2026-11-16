package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func requestLogger(ctx context.Context) *slog.Logger {
	return slog.Default().With("request_id", middleware.GetReqID(ctx))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := requestLogger(r.Context())
		response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()
		defer func() {
			status := response.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.InfoContext(r.Context(), "HTTP request",
				"event", "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(started).Milliseconds(),
				"proto", r.Proto,
				"size", response.BytesWritten(),
			)
		}()
		next.ServeHTTP(response, r)
	})
}
