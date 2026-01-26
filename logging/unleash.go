package logging

import (
	"log/slog"
	"strings"

	"github.com/Unleash/unleash-go-sdk/v5"
)

// SlogListener implements the unleash.Listener interface using slog for logging
type SlogListener struct {
	appName string
}

// OnError is called when an error occurs in the Unleash client
func (l *SlogListener) OnError(err error) {
	errMsg := err.Error()

	// Treat retry/backoff errors as warnings since they are transient
	// The SDK uses these phrases when backing off due to 429 or 5xx errors
	if strings.Contains(errMsg, "backing off") {
		slog.Warn("Unleash request retry for "+l.appName,
			slog.String("app_name", l.appName),
			slog.String("warning", errMsg),
		)
		return
	}

	slog.Error("Unleash error for "+l.appName,
		slog.String("app_name", l.appName),
		slog.String("error", errMsg),
	)
}

// OnWarning is called when a warning occurs in the Unleash client
func (l *SlogListener) OnWarning(warning error) {
	slog.Warn("Unleash warning for "+l.appName,
		slog.String("app_name", l.appName),
		slog.String("warning", warning.Error()),
	)
}

// OnReady is called when the Unleash client is ready
func (l *SlogListener) OnReady() {
	slog.Info("Unleash client ready for "+l.appName,
		slog.String("app_name", l.appName),
	)
}

// OnCount is called when feature toggles are counted
func (l *SlogListener) OnCount(name string, enabled bool) {
	slog.Debug("Unleash feature count for "+l.appName,
		slog.String("app_name", l.appName),
		slog.String("feature", name),
		slog.Bool("enabled", enabled),
	)
}

// OnSent is called when metrics are sent to the Unleash server
func (l *SlogListener) OnSent(payload unleash.MetricsData) {
	slog.Debug("Unleash metrics sent for "+l.appName,
		slog.String("app_name", l.appName),
		slog.Time("start", payload.Bucket.Start),
		slog.Time("stop", payload.Bucket.Stop),
		slog.Int("toggles", len(payload.Bucket.Toggles)),
	)
}

// OnRegistered is called when the client is registered with the Unleash server
func (l *SlogListener) OnRegistered(payload unleash.ClientData) {
	slog.Info("Unleash client registered for "+l.appName,
		slog.String("app_name", l.appName),
		slog.String("instance_id", payload.InstanceID),
		slog.String("sdk_version", payload.SDKVersion),
		slog.Any("strategies", payload.Strategies),
		slog.Time("started", payload.Started),
		slog.Int64("interval", payload.Interval),
	)
}

// NewSlogListener creates a new SlogListener with the given app name
func NewSlogListener(appName string) *SlogListener {
	return &SlogListener{
		appName: appName,
	}
}
