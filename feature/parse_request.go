package feature

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Unleash/unleash-go-sdk/v5"
	unleashcontext "github.com/Unleash/unleash-go-sdk/v5/context"
	"github.com/navikt/klage-unleash-proxy/clients"
	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/metrics"
	"github.com/navikt/klage-unleash-proxy/nais"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// featureRequest holds the validated and resolved values produced by parseRequest.
type featureRequest struct {
	featureName string
	req         Request
	client      *unleash.Client
	unleashCtx  unleashcontext.Context
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

// parseRequest extracts the feature name from the path, decodes the JSON body,
// validates all fields, resolves the Unleash client, and builds the Unleash
// context. If any step fails it writes an HTTP error response and returns false.
//
// errPrefix is prepended to metric error labels to distinguish REST from SSE errors.
func parseRequest(w http.ResponseWriter, r *http.Request, span trace.Span, log *slog.Logger, errPrefix string) (*featureRequest, bool) {
	// Extract feature name from path
	featureName := strings.TrimPrefix(r.URL.Path, PathPrefix)
	if featureName == "" {
		span.SetStatus(codes.Error, "missing feature name")
		span.SetAttributes(attribute.String("error.type", "missing_feature"))
		log.Warn("Missing feature name",
			"method", r.Method,
			"path", r.URL.Path,
		)
		metrics.RecordFeatureError(errPrefix + "missing_feature_name")
		http.Error(w, "Feature name is required", http.StatusBadRequest)
		return nil, false
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
		metrics.RecordFeatureError(errPrefix + "invalid_feature_name")
		http.Error(w, "Invalid feature name: must be URL-friendly, 1-100 characters, and not '.' or '..'", http.StatusBadRequest)
		return nil, false
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
		metrics.RecordFeatureError(errPrefix + "invalid_json_body")
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return nil, false
	}

	span.SetAttributes(
		attribute.String("request.app_name", req.AppName),
		attribute.String("request.pod_name", req.PodName),
	)

	// Validate appName is provided
	if req.AppName == "" {
		span.SetStatus(codes.Error, "missing app_name")
		span.SetAttributes(attribute.String("error.type", "missing_app_name"))
		log.Warn("Missing appName in request body",
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
		)
		metrics.RecordFeatureError(errPrefix + "missing_app_name")
		http.Error(w, fmt.Sprintf("appName is required in request body, must be one of the allowed inbound applications: %s", strings.Join(nais.InboundApps, ", ")), http.StatusBadRequest)
		return nil, false
	}

	// Get the Unleash client for the specified app
	client, ok := clients.Get(req.AppName)
	if !ok {
		span.SetStatus(codes.Error, "unknown app_name")
		span.SetAttributes(attribute.String("error.type", "unknown_app_name"))
		log.Warn("Unknown appName: "+req.AppName,
			"method", r.Method,
			"path", r.URL.Path,
			"feature", featureName,
			"app_name", req.AppName,
		)
		metrics.RecordFeatureError(errPrefix + "unknown_app_name")
		http.Error(w, fmt.Sprintf("Unknown appName: must be one of the allowed inbound applications: %s", strings.Join(nais.InboundApps, ", ")), http.StatusBadRequest)
		return nil, false
	}

	unleashCtx := unleashcontext.Context{
		Environment:   env.UnleashServerAPIEnv,
		UserId:        req.NavIdent,
		AppName:       req.AppName,
		RemoteAddress: r.RemoteAddr,
		Properties: map[string]string{
			"podName": req.PodName,
		},
	}

	return &featureRequest{
		featureName: featureName,
		req:         req,
		client:      client,
		unleashCtx:  unleashCtx,
	}, true
}
