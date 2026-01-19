package feature

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Unleash/unleash-go-sdk/v5"
	unleashcontext "github.com/Unleash/unleash-go-sdk/v5/context"
	"github.com/navikt/klage-unleash-proxy/env"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

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

// IsValidName validates the feature name according to Unleash rules:
// - Must be URL-friendly (encodeURIComponent(name) === name)
// - Cannot be "." or ".."
// - Must be between 1 and 100 characters
func IsValidName(name string) bool {
	if len(name) < 1 || len(name) > 100 {
		return false
	}
	if name == "." || name == ".." {
		return false
	}
	// Check if URL-friendly: encoded version should equal the original
	encoded := url.PathEscape(name)
	return encoded == name
}

// Handler handles feature check requests.
// It expects requests to POST or QUERY /features/{featureName} with a JSON body.
func Handler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	ctx := r.Context()

	// Start a span for the feature check
	ctx, span := tracer.Start(ctx, "featureHandler",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		),
	)
	defer span.End()

	if r.Method != http.MethodPost && r.Method != "QUERY" {
		span.SetStatus(codes.Error, "method not allowed")
		span.SetAttributes(attribute.String("error.type", "method_not_allowed"))
		slog.Warn("Method not allowed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract feature name from path
	featureName := strings.TrimPrefix(r.URL.Path, "/features/")
	if featureName == "" {
		span.SetStatus(codes.Error, "missing feature name")
		span.SetAttributes(attribute.String("error.type", "missing_feature"))
		slog.Warn("Missing feature name",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		http.Error(w, "Feature name is required", http.StatusBadRequest)
		return
	}

	span.SetAttributes(attribute.String("feature.name", featureName))

	// Validate feature name according to Unleash rules
	if !IsValidName(featureName) {
		span.SetStatus(codes.Error, "invalid feature name")
		span.SetAttributes(attribute.String("error.type", "invalid_feature"))
		slog.Warn("Invalid feature name",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("feature", featureName),
		)
		http.Error(w, "Invalid feature name: must be URL-friendly, 1-100 characters, and not '.' or '..'", http.StatusBadRequest)
		return
	}

	// Parse JSON body
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "invalid JSON body")
		span.RecordError(err)
		slog.Warn("Invalid JSON body",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("feature", featureName),
			slog.String("error", err.Error()),
		)
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("request.app_name", req.AppName),
		attribute.String("request.pod_name", req.PodName),
	)

	// CurrentTime is defaulted to now.
	unleashCtx := unleashcontext.Context{
		Environment:   env.UnleashServerAPIEnv,
		UserId:        req.NavIdent,
		AppName:       req.AppName,
		RemoteAddress: r.RemoteAddr,
		Properties: map[string]string{
			"podName": req.PodName,
		},
	}

	// Create a child span for the Unleash check
	_, unleashSpan := tracer.Start(ctx, "unleash.IsEnabled",
		trace.WithAttributes(
			attribute.String("feature.name", featureName),
			attribute.String("user_id", req.NavIdent),
			attribute.String("app_name", req.AppName),
			attribute.String("pod_name", req.PodName),
		),
	)
	enabled := unleash.IsEnabled(featureName, unleash.WithContext(unleashCtx))
	unleashSpan.SetAttributes(attribute.Bool("feature.enabled", enabled))
	unleashSpan.End()

	span.SetAttributes(attribute.Bool("feature.enabled", enabled))

	slog.Debug("Feature check",
		slog.String("feature", featureName),
		slog.Bool("enabled", enabled),
		slog.String("user_id", req.NavIdent),
		slog.String("app_name", req.AppName),
		slog.String("pod_name", req.PodName),
		slog.Int64("duration", time.Since(startTime).Milliseconds()),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{Enabled: enabled})
}
