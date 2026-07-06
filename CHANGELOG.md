# Changelog

## 1.0.0 (Unreleased)

Initial release.

- Query types: activities, sport totals, GPS track, metric (23 health/training metrics), splits, HR zones, gear, devices, personal records, HR/power zone settings
- Activity template-variable support for per-activity dashboards
- Five bundled dashboards: Athlete Overview, Activity, Health, Fitness, Athlete Comparison
- Activity start/end coordinates for map panels ("activities on map")
- Configurable units: speed (km/h, mph, m/s) and measurement system (metric or imperial)
- Reactive MFA flow (code verified against the pending login via a resource endpoint)
- Optional token file to persist the Garmin OAuth token across restarts
- Alerting support with response caching (5 min recent / 24 h historical) and request coalescing
- Long time ranges supported everywhere: requests are chunked to Garmin's per-endpoint range limits
- OpenTelemetry spans around all Garmin API calls, named per metric
