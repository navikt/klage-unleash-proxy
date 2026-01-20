package logging

import (
	"log/slog"

	"github.com/Unleash/unleash-go-sdk/v5"
)

// SlogListener implements the unleash.Listener interface using slog for logging
type SlogListener struct{}

// OnError is called when an error occurs in the Unleash client
func (l *SlogListener) OnError(err error) {
	slog.Error("Unleash error",
		slog.String("error", err.Error()),
	)
}

// OnWarning is called when a warning occurs in the Unleash client
func (l *SlogListener) OnWarning(warning error) {
	slog.Warn("Unleash warning",
		slog.String("warning", warning.Error()),
	)
}

// OnReady is called when the Unleash client is ready
func (l *SlogListener) OnReady() {
	slog.Info("Unleash client ready")
}

// OnCount is called when feature toggles are counted
func (l *SlogListener) OnCount(name string, enabled bool) {
	slog.Debug("Unleash feature count",
		slog.String("feature", name),
		slog.Bool("enabled", enabled),
	)
}

// OnSent is called when metrics are sent to the Unleash server
func (l *SlogListener) OnSent(payload unleash.MetricsData) {
	slog.Debug("Unleash metrics sent",
		slog.Time("start", payload.Bucket.Start),
		slog.Time("stop", payload.Bucket.Stop),
		slog.Int("toggles", len(payload.Bucket.Toggles)),
	)
}

// OnRegistered is called when the client is registered with the Unleash server
func (l *SlogListener) OnRegistered(payload unleash.ClientData) {
	slog.Info("Unleash client registered for "+payload.AppName,
		slog.String("app_name", payload.AppName),
		slog.String("instance_id", payload.InstanceID),
		slog.String("sdk_version", payload.SDKVersion),
		slog.Any("strategies", payload.Strategies),
		slog.Time("started", payload.Started),
		slog.Int64("interval", payload.Interval),
	)
}

// NewSlogListener creates a new SlogListener
func NewSlogListener() *SlogListener {
	return &SlogListener{}
}
