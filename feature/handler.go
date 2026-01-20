package feature

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Unleash/unleash-go-sdk/v5"
	unleashcontext "github.com/Unleash/unleash-go-sdk/v5/context"
	"github.com/navikt/klage-unleash-proxy/clients"
	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/logging"
	"github.com/navikt/klage-unleash-proxy/nais"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var PathPrefix = "/features/"

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

	log := logging.FromContext(ctx)

	if r.Method != http.MethodPost && r.Method != "QUERY" {
		span.SetStatus(codes.Error, "method not allowed")
		span.SetAttributes(attribute.String("error.type", "method_not_allowed"))
		log.Warn("Method not allowed",
			"method", r.Method,
			"path", r.URL.Path,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract feature name from path
	featureName := strings.TrimPrefix(r.URL.Path, PathPrefix)
	if featureName == "" {
		span.SetStatus(codes.Error, "missing feature name")
		span.SetAttributes(attribute.String("error.type", "missing_feature"))
		log.Warn("Missing feature name",
			"method", r.Method,
			"path", r.URL.Path,
		)
		http.Error(w, "Feature name is required", http.StatusBadRequest)
		return
	}

	span.SetAttributes(attribute.String("feature.name", featureName))

	// Validate feature name according to Unleash rules
	if !IsValidName(featureName) {
		span.SetStatus(codes.Error, "invalid feature name")
		span.SetAttributes(attribute.String("error.type", "invalid_feature"))
		log.Warn("Invalid feature name",
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
		)
		http.Error(w, "Invalid feature name: must be URL-friendly, 1-100 characters, and not '.' or '..'", http.StatusBadRequest)
		return
	}

	// Parse JSON body
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.SetStatus(codes.Error, "invalid JSON body")
		span.RecordError(err)
		log.Warn("Invalid JSON body",
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
			"error", err.Error(),
		)
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("request.app_name", req.AppName),
		attribute.String("request.pod_name", req.PodName),
	)

	// Validate app_name is provided
	if req.AppName == "" {
		span.SetStatus(codes.Error, "missing app_name")
		span.SetAttributes(attribute.String("error.type", "missing_app_name"))
		log.Warn("Missing app_name in request body",
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
		)
		http.Error(w, fmt.Sprintf("app_name is required in request body, must be one of the allowed inbound applications: %s", strings.Join(nais.InboundApps, ", ")), http.StatusBadRequest)
		return
	}

	// Get the Unleash client for the specified app
	client, ok := clients.Get(req.AppName)
	if !ok {
		span.SetStatus(codes.Error, "unknown app_name")
		span.SetAttributes(attribute.String("error.type", "unknown_app_name"))
		log.Warn("Unknown app_name: "+req.AppName,
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
			"app_name", req.AppName,
		)
		http.Error(w, fmt.Sprintf("Unknown app_name: must be one of the allowed inbound applications: %s", strings.Join(nais.InboundApps, ", ")), http.StatusBadRequest)
		return
	}

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
	enabled := client.IsEnabled(featureName, unleash.WithContext(unleashCtx))
	unleashSpan.SetAttributes(attribute.Bool("feature.enabled", enabled))
	unleashSpan.End()

	span.SetAttributes(attribute.Bool("feature.enabled", enabled))

	log.Debug(fmt.Sprintf("Feature check for %s - %s = %t", req.AppName, featureName, enabled),
		"feature", featureName,
		"enabled", enabled,
		"user_id", req.NavIdent,
		"app_name", req.AppName,
		"pod_name", req.PodName,
		"duration", time.Since(startTime).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{Enabled: enabled})
}
