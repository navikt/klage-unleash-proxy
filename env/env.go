package env

import "os"

// NAIS environment variables
var NaisAppName = os.Getenv("NAIS_APP_NAME")
var NaisClusterName = os.Getenv("NAIS_CLUSTER_NAME")

// Unleash environment variables
var UnleashServerAPIURL = os.Getenv("UNLEASH_SERVER_API_URL")
var UnleashServerAPIToken = os.Getenv("UNLEASH_SERVER_API_TOKEN")
var UnleashServerAPIEnv = os.Getenv("UNLEASH_SERVER_API_ENV")

// OpenTelemetry environment variables
var OtelServiceName = os.Getenv("OTEL_SERVICE_NAME")
var OtelServiceVersion = os.Getenv("OTEL_SERVICE_VERSION")
var OtelExporterOTLPEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

// Server environment variables
var Port = os.Getenv("PORT")

const DefaultServiceName = "klage-unleash-proxy"
const DefaultPort = "8080"
