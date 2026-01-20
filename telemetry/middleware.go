package telemetry

import (
	"net/http"
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

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
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
