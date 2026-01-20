package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/navikt/klage-unleash-proxy/clients"
	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/feature"
	"github.com/navikt/klage-unleash-proxy/logging"
	"github.com/navikt/klage-unleash-proxy/nais"
	"github.com/navikt/klage-unleash-proxy/telemetry"
)

var okBytes = []byte("OK")

func init() {
	// Initialize JSON logger
	logging.Initialize()
}

func livenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write(okBytes)
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	if !clients.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("NOT READY"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(okBytes)
}

func initializeClients() {
	if err := clients.Initialize(); err != nil {
		slog.Error("Failed to initialize Unleash clients",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	slog.Info(fmt.Sprintf("All %d Unleash clients ready", len(nais.InboundApps)))
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry
	otelConfig := telemetry.ConfigFromEnv()
	otelInstance, err := telemetry.Initialize(ctx, otelConfig)
	if err != nil {
		slog.Error("Failed to initialize OpenTelemetry: "+err.Error(),
			slog.String("error", err.Error()),
		)
		// Continue without telemetry rather than failing
	}

	// Initialize tracer after OpenTelemetry initialization
	feature.InitTracer()

	// Create OpenTelemetry middleware
	otelMiddleware, err := telemetry.NewMiddleware(otelInstance != nil)
	if err != nil {
		slog.Error("Failed to create OpenTelemetry middleware: "+err.Error(),
			slog.String("error", err.Error()),
		)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/isAlive", livenessHandler)
	mux.HandleFunc("/isReady", readinessHandler)

	mux.HandleFunc(feature.PathPrefix, feature.Handler)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	port := env.Port
	if port == "" {
		port = env.DefaultPort
	}

	// Build the handler chain
	// Order matters: OTel middleware must run first (outermost) to create the trace context,
	// then logging middleware can access the trace ID from the context
	var handler http.Handler = mux
	handler = logging.Middleware(handler)
	if otelMiddleware != nil {
		handler = otelMiddleware.Handler(handler)
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	// Start server in a goroutine so we can initialize clients while serving health checks
	go func() {
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
	}()

	// Initialize Unleash clients after server is started
	initializeClients()

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

		// Close all Unleash clients
		clients.Close()

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

	// Wait for shutdown signal
	<-ctx.Done()

	slog.Info("Server shutdown complete")
}
