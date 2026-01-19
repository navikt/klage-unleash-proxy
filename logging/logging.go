package logging

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// Initialize sets up the default JSON logger
func Initialize() *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)
	return logger
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// shouldSkipLogging returns true for health check endpoints that should not be logged
func shouldSkipLogging(path string) bool {
	return path == "/isAlive" || path == "/isReady"
}

// Middleware returns an HTTP middleware that logs each request with timing information
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for health check endpoints
		if shouldSkipLogging(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Get trace ID from context if available
		spanCtx := trace.SpanContextFromContext(r.Context())
		logAttrs := []any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.statusCode),
			slog.Int64("duration", duration.Milliseconds()),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
		}

		if spanCtx.HasTraceID() {
			logAttrs = append(logAttrs, slog.String("trace_id", spanCtx.TraceID().String()))
		}
		if spanCtx.HasSpanID() {
			logAttrs = append(logAttrs, slog.String("span_id", spanCtx.SpanID().String()))
		}

		slog.Info("Request completed", logAttrs...)
	})
}
