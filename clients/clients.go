package clients

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/Unleash/unleash-go-sdk/v5"
	"github.com/navikt/klage-unleash-proxy/env"
	"github.com/navikt/klage-unleash-proxy/logging"
	"github.com/navikt/klage-unleash-proxy/nais"
)

var (
	// url is the Unleash server API url used by all clients.
	url       = env.UnleashServerAPIURL + "/api"
	clientMap = make(map[string]*unleash.Client)
	mu        sync.RWMutex
	ready     atomic.Bool
)

// Ready returns true if all Unleash clients have been initialized.
func Ready() bool {
	return ready.Load()
}

// Initialize creates and initializes Unleash clients for all inbound applications.
// This should be called once at startup.
func Initialize() error {
	slog.Info(fmt.Sprintf("Initializing Unleash clients for %d applications", len(nais.InboundApps)),
		slog.String("url", url),
		slog.String("environment", env.UnleashServerAPIEnv),
		slog.Bool("has_api_key", env.UnleashServerAPIToken != ""),
		slog.Int("count", len(nais.InboundApps)),
		slog.Any("apps", nais.InboundApps),
	)

	var wg sync.WaitGroup
	errChan := make(chan error, len(nais.InboundApps))

	for _, appName := range nais.InboundApps {
		wg.Add(1)
		go func(app string) {
			defer wg.Done()

			slog.Info("Initializing Unleash client for "+app,
				slog.String("app_name", app),
				slog.String("url", url),
				slog.String("environment", env.UnleashServerAPIEnv),
			)

			client, err := unleash.NewClient(
				unleash.WithListener(logging.NewSlogListener(app)),
				unleash.WithAppName(app),
				unleash.WithUrl(url),
				unleash.WithCustomHeaders(http.Header{"Authorization": {env.UnleashServerAPIToken}}),
			)
			if err != nil {
				errChan <- fmt.Errorf("failed to create Unleash client for %s: %w", app, err)
				return
			}

			client.WaitForReady()

			mu.Lock()
			clientMap[app] = client
			mu.Unlock()

			slog.Info("Unleash client ready for "+app,
				slog.String("app_name", app),
			)
		}(appName)
	}

	wg.Wait()
	close(errChan)

	// Collect any errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to initialize some Unleash clients: %v", errs)
	}

	ready.Store(true)
	return nil
}

// Get returns the Unleash client for the given app name.
// Returns nil and false if the app is not found.
func Get(appName string) (*unleash.Client, bool) {
	mu.RLock()
	defer mu.RUnlock()
	client, ok := clientMap[appName]
	return client, ok
}

// Close closes all Unleash clients.
// This should be called during graceful shutdown.
func Close() {
	mu.Lock()
	defer mu.Unlock()

	for appName, client := range clientMap {
		slog.Info("Closing Unleash client",
			slog.String("app_name", appName),
		)
		client.Close()
	}

	clientMap = make(map[string]*unleash.Client)
}

// IsValidApp checks if the given app name is in the list of allowed inbound apps.
func IsValidApp(appName string) bool {
	return slices.Contains(nais.InboundApps, appName)
}
