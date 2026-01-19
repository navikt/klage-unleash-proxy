package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Unleash/unleash-go-sdk/v5"
	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/feature"
	"github.com/navikt/klage-unleash-proxy/logging"
	"github.com/navikt/klage-unleash-proxy/telemetry"
)

var URL = env.UnleashServerAPIURL + "/api"

func init() {
	// Initialize JSON logger
	logging.Initialize()

	slog.Info("Initializing Unleash client",
		slog.String("appName", env.NaisAppName),
		slog.String("url", URL),
		slog.String("environment", env.UnleashServerAPIEnv),
		slog.Bool("hasApiKey", env.UnleashServerAPIToken != ""),
	)

	unleash.Initialize(
		unleash.WithListener(logging.NewSlogListener()),
		unleash.WithAppName(env.NaisAppName),
		unleash.WithUrl(URL),
		unleash.WithCustomHeaders(http.Header{"Authorization": {env.UnleashServerAPIToken}}),
	)

	// Blocks until the default client is ready
	unleash.WaitForReady()

	slog.Info("Unleash client ready")
}

var okBytes = []byte("OK")

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write(okBytes)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry
	otelConfig := telemetry.ConfigFromEnv()
	otelInstance, err := telemetry.Initialize(ctx, otelConfig)
	if err != nil {
		slog.Error("Failed to initialize OpenTelemetry",
			slog.String("error", err.Error()),
		)
		// Continue without telemetry rather than failing
	}

	// Initialize tracer after OpenTelemetry initialization
	feature.InitTracer()

	// Create OpenTelemetry middleware
	otelMiddleware, err := telemetry.NewMiddleware(otelInstance != nil)
	if err != nil {
		slog.Error("Failed to create OpenTelemetry middleware",
			slog.String("error", err.Error()),
		)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/isAlive", healthHandler)
	mux.HandleFunc("/isReady", healthHandler)

	mux.HandleFunc("/features/", feature.Handler)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	port := env.Port
	if port == "" {
		port = env.DefaultPort
	}

	// Build the handler chain
	var handler http.Handler = mux
	if otelMiddleware != nil {
		handler = otelMiddleware.Handler(handler)
	}
	handler = logging.Middleware(handler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// Handle graceful shutdown
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChannel
		slog.Info("Received shutdown signal, shutting down gracefully...")

		// Create a deadline for graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		// Shutdown the HTTP server
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error",
				slog.String("error", err.Error()),
			)
		}

		// Shutdown OpenTelemetry
		if otelInstance != nil {
			if err := otelInstance.Shutdown(shutdownCtx); err != nil {
				slog.Error("OpenTelemetry shutdown error",
					slog.String("error", err.Error()),
				)
			}
		}

		cancel()
	}()

	slog.Info("Starting server",
		slog.String("port", port),
		slog.Bool("otel_enabled", otelInstance != nil),
	)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	slog.Info("Server shutdown complete")
}
