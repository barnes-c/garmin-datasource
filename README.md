# Garmin Connect data source for Grafana

Grafana backend datasource plugin that visualizes Garmin Connect activities, GPS tracks, and health metrics — no database or sync jobs required.

**User documentation** (features, setup, query types) lives in [src/README.md](src/README.md), which is what the Grafana plugin catalog displays.

## Development

Requirements: Go 1.26+, Node 22+, Docker (or Podman), [Mage](https://magefile.org/).

```bash
npm install
npm run dev                 # frontend, watch mode
mage -v build:linuxARM64    # backend for the dev container (use build:linuxAMD64 on x86)
docker compose up           # Grafana at http://localhost:3000 (admin/admin) + Tempo for traces
```

Credentials for the provisioned datasource go into `.env` (gitignored):

```txt
GARMIN_EMAIL=you@example.com
GARMIN_PASSWORD=...
# For MFA accounts: complete MFA once via the Verify button; the token persists in .tokens/
```

The dev environment provisions the datasource, a Tempo instance, and a **Garmin Datasource Traces** dashboard showing every Garmin API call with durations (the plugin emits OpenTelemetry spans; in production they export wherever the host Grafana's tracing is configured).

### Tests

```bash
go test ./pkg/...                 # backend: fixture-server + unit tests, no Garmin account needed
npm run test:ci                   # frontend
npm run e2e                       # Playwright against the running dev Grafana
```

### Architecture notes

- `pkg/plugin/datasource.go` — query dispatch, auth state machine (reactive MFA via a `/mfa` resource endpoint, exponential login backoff), frame cache (5 min recent / 24 h historical tiers), singleflight coalescing.
- `pkg/plugin/metrics.go` — metric registry; adding a metric is one entry (`fetch` for single series, `fetchFrame` for multi-field). Day-keyed Garmin endpoints go through the generic `perDay` fan-out (bounded concurrency, 93-day cap, partial-failure notices).
- `pkg/plugin/tables.go` — table-shaped queries (gear, devices, personal records, splits, HR/power zone configs).
- Speeds are converted from Garmin's m/s at frame-build time according to the datasource's speed-unit setting; Grafana units are display-only and cannot convert.
- Dashboards in `src/dashboards/` are bundled via `plugin.json` includes. The activity dashboard deliberately uses trend panels (distance x-axis) and a fit-view geomap so panels are independent of the dashboard time range.

## Release

Push a `v*` tag; the release workflow builds, signs (requires the `GRAFANA_ACCESS_POLICY_TOKEN` repository secret), and drafts a GitHub release.
