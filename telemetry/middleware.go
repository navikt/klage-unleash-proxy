package telemetry

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/navikt/klage-unleash-proxy/telemetry"
)

var traceparentPattern = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// traceparentValidator validates traceparent values against the W3C Trace Context spec,
// rejecting version ff, all-zero trace-id, and all-zero parent-id.
type traceparentValidator struct{}

func (traceparentValidator) MatchString(value string) bool {
	if !traceparentPattern.MatchString(value) {
		return false
	}
	if value[0:2] == "ff" {
		return false
	}
	traceID, err := trace.TraceIDFromHex(value[3:35])
	if err != nil || !traceID.IsValid() {
		return false
	}
	parentID, err := trace.SpanIDFromHex(value[36:52])
	if err != nil || !parentID.IsValid() {
		return false
	}
	return true
}

var validTraceparent = traceparentValidator{}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter so http.ResponseController
// can discover optional interfaces like http.Flusher.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Middleware provides OpenTelemetry instrumentation for HTTP handlers
type Middleware struct {
	tracer          trace.Tracer
	requestCounter  metric.Int64Counter
	requestDuration metric.Float64Histogram
	enabled         bool
}

// NewMiddleware creates a new OpenTelemetry middleware
func NewMiddleware(enabled bool) (*Middleware, error) {
	m := &Middleware{
		enabled: enabled,
	}

	if !enabled {
		return m, nil
	}

	m.tracer = otel.Tracer(instrumentationName)

	meter := otel.Meter(instrumentationName)

	var err error

	// Create request counter
	m.requestCounter, err = meter.Int64Counter(
		"http.server.request_count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	// Create request duration histogram
	m.requestDuration, err = meter.Float64Histogram(
		"http.server.duration",
		metric.WithDescription("HTTP request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// Handler wraps an http.Handler with OpenTelemetry instrumentation
func (m *Middleware) Handler(next http.Handler) http.Handler {
	if !m.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Support traceparent as a query parameter for SSE clients that cannot set headers (e.g. EventSource).
		if r.Header.Get("Traceparent") == "" && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			if tp := r.URL.Query().Get("traceparent"); tp != "" && validTraceparent.MatchString(tp) {
				r.Header.Set("Traceparent", tp)
			}
		}

		// Extract trace context from incoming request
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Start a new span
		ctx, span := m.tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				HTTPRequestMethodKey.String(r.Method),
				URLPath(r.URL.Path),
				URLScheme(scheme(r)),
				ServerAddress(r.Host),
				UserAgentOriginal(r.UserAgent()),
				ClientAddress(r.RemoteAddr),
			),
		)
		defer span.End()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Call the next handler with the updated context
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Record the status code in the span
		span.SetAttributes(HTTPResponseStatusCode(wrapped.statusCode))

		// Calculate duration
		duration := time.Since(start).Seconds()

		// Common attributes for metrics
		attrs := []attribute.KeyValue{
			HTTPRequestMethodKey.String(r.Method),
			HTTPRoute(r.URL.Path),
			HTTPResponseStatusCode(wrapped.statusCode),
		}

		// Record metrics
		m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
	})
}

// scheme returns the HTTP scheme (http or https) for the request
func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// Check common proxy headers
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}
