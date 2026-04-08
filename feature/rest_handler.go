package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Unleash/unleash-go-sdk/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/navikt/klage-unleash-proxy/metrics"
)

// handleREST handles a single feature check via POST or QUERY with a JSON body.
func handleREST(w http.ResponseWriter, r *http.Request, ctx context.Context, span trace.Span, log *slog.Logger) {
	startTime := time.Now()

	fr, ok := parseRequest(w, r, span, log, "rest_")
	if !ok {
		return
	}

	// Create a child span for the Unleash check
	_, unleashSpan := tracer.Start(ctx, "unleash.IsEnabled",
		trace.WithAttributes(
			attribute.String("feature.name", fr.featureName),
			attribute.String("user_id", fr.req.NavIdent),
			attribute.String("app_name", fr.req.AppName),
			attribute.String("pod_name", fr.req.PodName),
		),
	)
	enabled := fr.client.IsEnabled(fr.featureName, unleash.WithContext(fr.unleashCtx))
	unleashSpan.SetAttributes(attribute.Bool("feature.enabled", enabled))
	unleashSpan.End()

	span.SetAttributes(attribute.Bool("feature.enabled", enabled))

	// Record Prometheus metrics
	duration := time.Since(startTime)
	metrics.RecordFeatureRequest(fr.featureName, fr.req.AppName, enabled, duration)

	log.Debug(fmt.Sprintf("Feature check for %s - %s = %t", fr.req.AppName, fr.featureName, enabled),
		"feature", fr.featureName,
		"enabled", enabled,
		"user_id", fr.req.NavIdent,
		"app_name", fr.req.AppName,
		"pod_name", fr.req.PodName,
		"duration", duration.Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(Response{Enabled: enabled})
}
