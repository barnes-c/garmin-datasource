# Changelog

## 1.0.0 (2026-07-15)

Initial release.

- Query types: activities, sport totals, GPS track, metric (23 health/training metrics), splits, power meter samples, HR zones, power zones, gear, devices, personal records, HR/power zone settings
- Six bundled dashboards: Athlete Overview, Activity, Cycling, Health, Fitness, Athlete Comparison
- Activity start/end coordinates for map panels ("activities on map")
- Configurable units: speed (km/h, mph, m/s) and measurement system (metric or imperial)
- Reactive MFA flow (code verified against the pending login via a resource endpoint)
- Optional session token (stored encrypted in secureJsonData) to persist the Garmin OAuth login across restarts
- Alerting support with response caching (5 min recent / 24 h historical) and request coalescing
- OpenTelemetry spans around all Garmin API calls, named per metric
