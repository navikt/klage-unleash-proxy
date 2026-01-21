package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/navikt/klage-unleash-proxy/env"
)

var (
	defaultLabels = prometheus.Labels{
		"app":       env.NaisAppName,
		"version":   env.AppVersion,
		"namespace": env.NaisNamespace,
		"pod_name":  env.NaisPodName,
	}

	// Create a wrapped registry with default labels
	registry = prometheus.WrapRegistererWith(defaultLabels, prometheus.DefaultRegisterer)

	// Use promauto.With() to register metrics with the wrapped registry
	factory = promauto.With(registry)

	// FeatureRequestsTotal counts the total number of feature check requests
	FeatureRequestsTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "feature_requests_total",
			Help: "Total number of feature check requests, with state",
		},
		[]string{"feature", "app_name", "enabled"},
	)

	// FeatureRequestDuration tracks the duration of feature check requests
	FeatureRequestDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "feature_request_duration_seconds",
			Help: "Duration of feature check requests in seconds",
			// Custom buckets for sub-millisecond cached lookups: 500µs, 1ms, 5ms, 10ms, 20ms, 30ms, 40ms, 50ms, 75ms, 100ms, 125ms, 150ms, 200ms
			Buckets: []float64{0.005, 0.01, 0.02, 0.03, 0.04, 0.05, 0.075, 0.1, 0.125, 0.15, 0.2},
		},
		[]string{"feature", "app_name"},
	)

	// FeatureRequestErrors counts errors during feature checks
	FeatureRequestErrors = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "feature_request_errors_total",
			Help: "Total number of errors during feature check requests",
		},
		[]string{"error_type"},
	)
)

// RecordFeatureRequest records metrics for a successful feature check
func RecordFeatureRequest(feature, appName string, enabled bool, duration time.Duration) {
	FeatureRequestsTotal.WithLabelValues(feature, appName, strconv.FormatBool(enabled)).Inc()
	FeatureRequestDuration.WithLabelValues(feature, appName).Observe(duration.Seconds())
}

// RecordFeatureError records an error during feature check
func RecordFeatureError(errorType string) {
	FeatureRequestErrors.WithLabelValues(errorType).Inc()
}
