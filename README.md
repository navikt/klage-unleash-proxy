# Klage Unleash Proxy

A lightweight Go proxy service that provides feature flag lookups from [Unleash](https://www.getunleash.io/).

[Team Klage - Unleash Web UI](https://klage-unleash-web.iap.nav.cloud.nais.io)

## Overview

This service acts as a shared Unleash client for multiple applications. Instead of each application maintaining its own Unleash SDK connection, they can query this proxy to check feature flag states.

## Allowed Applications

The list of applications allowed to query this proxy is defined in [`nais/nais.yaml`](nais/nais.yaml) under `spec.accessPolicy.inbound.rules`.

To add a new application, update the inbound rules in the NAIS configuration.

## API

### Check Feature Flag

```
QUERY/POST /features/{featureName}
Content-Type: application/json

{
  "navIdent": "A123456",
  "appName": "kabal-api",
  "podName": "kabal-api-abc123"
}
```

**Request Body**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `navIdent` | string | No | User identifier for user-specific feature toggles |
| `appName` | string | Yes | Name of the calling application (must match the NAIS application name) |
| `podName` | string | No | Pod name of the calling application |

**Response:**

```json
{
  "enabled": true
}
```

**Status Codes:**

- `200 OK`: Feature flag status returned
- `400 Bad Request`: Invalid feature name, missing `appName`, or unknown application
- `405 Method Not Allowed`: Only `POST` and `QUERY` methods are accepted

### Health Endpoints

- `GET /isAlive` - Liveness probe (always returns 200 when server is running)
- `GET /isReady` - Readiness probe (returns 200 when all Unleash clients are initialized)

## Configuration

The service is configured via environment variables:

| Variable | Description |
|----------|-------------|
| `UNLEASH_SERVER_API_URL` | Unleash server URL |
| `UNLEASH_SERVER_API_TOKEN` | API token for Unleash authentication |
| `UNLEASH_SERVER_API_ENV` | Unleash environment |
| `PORT` | Server port (default: `8080`) |
| `NAIS_APP_NAME` | Application name (set by NAIS) |
| `NAIS_CLUSTER_NAME` | Cluster name (set by NAIS) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry collector endpoint |

## Development

### Prerequisites

- Go 1.25 or later

### Build

```sh
go build -o server .
```

### Run tests

```sh
go test ./...
```
