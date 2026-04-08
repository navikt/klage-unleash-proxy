package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Unleash/unleash-go-sdk/v5"
	"github.com/navikt/klage-unleash-proxy/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// Polling interval for feature toggle changes.
	pollInterval = 2 * time.Second
	// Heartbeat interval to keep SSE connections alive through proxies/load balancers.
	heartbeatInterval = 30 * time.Second
)

// SSEEvent represents a Server-Sent Event for feature toggle changes.
type SSEEvent struct {
	Traceparent string `json:"traceparent"`
	Enabled     bool   `json:"enabled"`
}

// handleSSE serves an SSE stream that polls a feature toggle and pushes
// changes to the client as Server-Sent Events with JSON data.
func handleSSE(w http.ResponseWriter, r *http.Request, ctx context.Context, span trace.Span, log *slog.Logger) {
	startTime := time.Now()

	parsed, ok := parseRequest(w, r, span, log, "sse_")
	if !ok {
		return
	}

	featureName := parsed.featureName
	appName := parsed.req.AppName

	// Record SSE-specific span attributes
	span.SetAttributes(
		attribute.String("feature.name", featureName),
		attribute.String("app_name", appName),
		attribute.String("user_id", parsed.req.NavIdent),
		attribute.String("pod_name", parsed.req.PodName),
		attribute.String("poll_interval", pollInterval.String()),
	)

	// Verify that the response writer supports flushing (required for SSE)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		span.SetStatus(codes.Error, "streaming not supported")
		span.SetAttributes(attribute.String("error.type", "streaming_not_supported"))
		log.Error("Streaming not supported by response writer",
			"feature", featureName,
			"app_name", appName,
		)
		metrics.RecordFeatureError("sse_streaming_not_supported")
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	log.Info("SSE subscription started",
		"feature", featureName,
		"app_name", appName,
		"nav_ident", parsed.req.NavIdent,
		"pod_name", parsed.req.PodName,
	)

	span.AddEvent("connection_started")

	metrics.RecordSSEConnection(featureName, appName)

	// Track the previous state so we only send events on changes.
	// Use a pointer to distinguish "no previous value" from false.
	var lastEnabled *bool
	var eventsSent int

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// recordDisconnect records final span attributes and metrics when the connection closes.
	recordDisconnect := func(reason string) {
		duration := time.Since(startTime)
		span.SetAttributes(
			attribute.Int("events_sent", eventsSent),
			attribute.String("duration", duration.String()),
			attribute.String("disconnect_reason", reason),
		)
		span.AddEvent("connection_ended", trace.WithAttributes(
			attribute.String("reason", reason),
			attribute.Int("events_sent", eventsSent),
			attribute.String("duration", duration.String()),
		))
		metrics.RecordSSEDisconnection(featureName, appName, duration)
	}

	lastWrite := time.Now()

	// sendHeartbeat writes an SSE comment to keep the connection alive.
	// Returns false if the write fails (client disconnected).
	sendHeartbeat := func() bool {
		_, writeErr := fmt.Fprint(w, ": heartbeat\n\n")
		if writeErr != nil {
			span.SetStatus(codes.Error, "heartbeat write failed")
			span.RecordError(writeErr)
			log.Debug("SSE client disconnected (heartbeat error)",
				"feature", featureName,
				"app_name", appName,
				"error", writeErr.Error(),
			)
			return false
		}
		rc.Flush()
		lastWrite = time.Now()
		return true
	}

	// sendIfChanged checks the current toggle value and writes an SSE event
	// when it differs from the last known value. Returns false when the
	// connection should be closed (write error or marshal failure).
	sendIfChanged := func() bool {
		enabled := parsed.client.IsEnabled(featureName, unleash.WithContext(parsed.unleashCtx))

		if lastEnabled != nil && *lastEnabled == enabled {
			return true // no change, keep going
		}

		span.AddEvent("value_changed", trace.WithAttributes(
			attribute.Bool("feature.enabled", enabled),
		))

		lastEnabled = &enabled

		carrier := propagation.MapCarrier{}
		propagation.TraceContext{}.Inject(ctx, carrier)

		event := SSEEvent{
			Traceparent: carrier.Get("traceparent"),
			Enabled:     enabled,
		}

		data, err := json.Marshal(event)
		if err != nil {
			span.SetStatus(codes.Error, "failed to marshal SSE event")
			span.RecordError(err)
			log.Error("Failed to marshal SSE event",
				"feature", featureName,
				"error", err.Error(),
			)
			return false
		}

		// Write SSE formatted event: "event: toggle\ndata: {...}\n\n"
		_, writeErr := fmt.Fprintf(w, "event: toggle\ndata: %s\n\n", data)
		if writeErr != nil {
			span.SetStatus(codes.Error, "SSE write failed")
			span.RecordError(writeErr)
			log.Debug("SSE client disconnected (write error)",
				"feature", featureName,
				"app_name", appName,
				"error", writeErr.Error(),
			)
			return false
		}

		rc.Flush()

		lastWrite = time.Now()
		eventsSent++

		metrics.RecordSSEEvent(featureName, appName, enabled)

		span.AddEvent("event_sent", trace.WithAttributes(
			attribute.Bool("feature.enabled", enabled),
			attribute.Int("event_number", eventsSent),
		))

		log.Debug(fmt.Sprintf("SSE event sent for %s - %s = %t", appName, featureName, enabled),
			"feature", featureName,
			"enabled", enabled,
			"app_name", appName,
			"event_number", eventsSent,
		)

		return true
	}

	// Send the initial state immediately.
	if !sendIfChanged() {
		recordDisconnect("send_error")
		return
	}

	// Poll for changes until the client disconnects.
	for {
		select {
		case <-ctx.Done():
			log.Info("SSE subscription ended (client disconnected)",
				"feature", featureName,
				"app_name", appName,
				"duration", time.Since(startTime).String(),
				"events_sent", eventsSent,
			)
			recordDisconnect("client_disconnected")
			return
		case <-ticker.C:
			if !sendIfChanged() {
				recordDisconnect("send_error")
				return
			}
			if time.Since(lastWrite) >= heartbeatInterval {
				if !sendHeartbeat() {
					recordDisconnect("heartbeat_error")
					return
				}
			}
		}
	}
}
