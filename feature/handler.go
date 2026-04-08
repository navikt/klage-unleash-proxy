package feature

import (
	"net/http"
	"strings"

	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/logging"
	"github.com/navikt/klage-unleash-proxy/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const sseAcceptHeader = "text/event-stream"

var PathPrefix = "/features/"

var tracer trace.Tracer

var serverHeader = env.NaisAppName + "/" + env.AppVersion

// InitTracer initializes the tracer after OpenTelemetry setup.
// Call this after telemetry.Initialize() to ensure proper tracing.
func InitTracer() {
	tracer = otel.Tracer(env.NaisAppName)
}

// Request represents the JSON body for feature check requests.
type Request struct {
	NavIdent string `json:"navIdent"`
	AppName  string `json:"appName"`
	PodName  string `json:"podName"`
}

// Response represents the JSON response for feature check requests.
type Response struct {
	Enabled bool `json:"enabled"`
}

// Handler handles feature check requests via POST or QUERY.
//
// It supports two modes, distinguished by the Accept header:
//   - text/event-stream: opens an SSE stream that pushes toggle changes.
//   - All other values: returns a single JSON feature check response.
func Handler(w http.ResponseWriter, r *http.Request) {
	// Add version headers to all responses
	w.Header().Set("Server", serverHeader)
	w.Header().Set("App-Version", env.AppVersion)

	ctx := r.Context()

	// Start a span for the feature check
	ctx, span := tracer.Start(ctx, "featureHandler",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		),
	)
	defer span.End()

	log := logging.FromContext(ctx)

	if r.Method != http.MethodPost && r.Method != "QUERY" {
		span.SetStatus(codes.Error, "method not allowed")
		span.SetAttributes(attribute.String("error.type", "method_not_allowed"))
		log.Warn("Method not allowed",
			"method", r.Method,
			"path", r.URL.Path,
		)
		metrics.RecordFeatureError("method_not_allowed")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// SSE subscription: Accept: text/event-stream
	if strings.Contains(r.Header.Get("Accept"), sseAcceptHeader) {
		handleSSE(w, r, ctx, span, log)
		return
	}

	// REST: single feature check
	handleREST(w, r, ctx, span, log)
}
