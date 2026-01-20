// Package telemetry provides OpenTelemetry instrumentation utilities.
//
// This file centralizes the semconv version to ensure consistency across
// the codebase. Update the import path here when upgrading OTel SDK.
package telemetry

// Use the latest semconv version bundled with the OTel SDK.
// Since we use resource.New() instead of resource.Merge(resource.Default(), ...),
// we avoid schema URL conflicts with the SDK's internal semconv version.
import semconv "go.opentelemetry.io/otel/semconv/v1.38.0"

// Re-export semconv constants and functions used in this project.
// This ensures all telemetry code uses the same schema version.

// Schema URL for resource creation
const SchemaURL = semconv.SchemaURL

// Service attributes
var (
	ServiceName           = semconv.ServiceName
	ServiceVersion        = semconv.ServiceVersion
	DeploymentEnvironment = semconv.DeploymentEnvironmentName
)

// HTTP attributes
var (
	HTTPRequestMethodKey   = semconv.HTTPRequestMethodKey
	HTTPRoute              = semconv.HTTPRoute
	HTTPResponseStatusCode = semconv.HTTPResponseStatusCode
	URLPath                = semconv.URLPath
	URLScheme              = semconv.URLScheme
	ServerAddress          = semconv.ServerAddress
	UserAgentOriginal      = semconv.UserAgentOriginal
	ClientAddress          = semconv.ClientAddress
)
