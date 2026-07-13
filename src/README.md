# Garmin Connect data source for Grafana

Visualize your Garmin Connect activities, GPS tracks, and health metrics in Grafana — no database, no sync jobs. The plugin talks to Garmin Connect directly from the Grafana backend, so setup is: install, enter your Garmin credentials, done.

![Activity dashboard](https://raw.githubusercontent.com/barnes-c/garmin-datasource/main/src/img/screenshots/activity.png)

## Features

- **GPS tracks on the Geomap panel** — routes with per-point heart rate, speed (km/h), cumulative distance, and elevation, ready for the Route layer
- **Activities table** over the dashboard time range, with an optional activity-type filter
- **23 health & training metrics** as time series: steps, sleep (with stages), Body Battery, stress, HRV, SpO2, respiration, resting heart rate, hydration, intensity minutes, floors, weight & body composition, VO2max, training readiness, fitness age, endurance/hill score, running tolerance, blood pressure, race predictions, lactate threshold, cycling FTP
- **Per-activity analysis**: lap splits, power meter samples, time in HR and power zones
- **Gear, devices, and personal records** tables
- **Activity template variable** for building per-activity dashboards
- **Six bundled dashboards** (Athlete Overview, Activity, Cycling, Health, Fitness, Athlete Comparison) — import them from the data source's *Dashboards* tab
- **Alerting support** — alert on resting HR, HRV, weight, missed activities, and more
- Built-in response caching and request coalescing to stay polite to Garmin's API

## Getting started

1. Install the plugin and add a **Garmin Connect** data source.
2. Enter your Garmin Connect **email** and **password** (stored encrypted by Grafana).
3. Recommended: set a **token file** path (e.g. `/var/lib/grafana/garmin_token.json`). The OAuth token is then reused across Grafana restarts, so you won't need to log in (or redo MFA) again. Without it, tokens are kept in memory only.
4. Click **Save & test**.

### Provisioning

```yaml
apiVersion: 1
datasources:
  - name: Garmin Connect
    type: barnesc-garminconnect-datasource
    access: proxy
    jsonData:
      email: $GARMIN_EMAIL
      tokenFile: /var/lib/grafana/garmin_token.json
      speedUnit: kmh # kmh | mph | ms
      unitSystem: metric # metric | imperial
    secureJsonData:
      password: $GARMIN_PASSWORD
```

### Accounts with MFA

If your account has multi-factor authentication, **Save & test** will report that Garmin sent a code to your email. Enter the code in the **MFA code** field and click **Verify** — the pending login completes without a new code being triggered. With a token file configured this is a one-time step.

For **fully headless setups** (provisioned Grafana, nobody to click Verify), note that an MFA code cannot be known in advance — Garmin only emails it in response to a login attempt, so a code cannot be provisioned. Instead, **provision the token**: complete the MFA login once anywhere (this plugin's UI, or [garmin_exporter](https://github.com/barnes-c/garmin_exporter) — the token file format is shared), copy the resulting token file to the server, and set `tokenFile`. The login resumes the token and MFA never happens on the headless host.

## Query types

| Query type | Returns |
|---|---|
| Activities | Table of activities in the dashboard time range (distance, duration, elevation, calories, HR, speed) |
| Sport totals | Distance, time and activity count per sport in the dashboard time range |
| Track | GPS trackpoints of one activity: time, lat, lon, elevation, heart rate, speed, cumulative distance |
| Metric | One health/training metric over the dashboard time range |
| Splits | Lap splits of one activity |
| Power | Power meter samples of one activity as a time series (W) |
| HR zones | Time in heart rate zones of one activity |
| Power zones | Time in power zones of one activity |
| Gear | Registered gear with lifetime distance and activity count |
| Devices | Registered Garmin devices with firmware and registration date |
| Personal records | All personal records, formatted per record type |

`Track`, `Splits`, `Power`, `HR zones`, and `Power zones` take an activity id and support dashboard variables (e.g. `$activity`). To create an activity picker, add a *query* variable — the editor lets you filter by activity type and limit the list; it offers the activities of the last year, independent of the dashboard time range. `Track` and `Power` queries have a **Fit time range** option: when the selected activity changes, the dashboard time range automatically snaps to the activity's recording window.

## Bundled dashboards

Open the data source's configuration page → **Dashboards** tab → import:

- **Garmin Athlete Overview** — training totals, distance/elevation trends, sleep, Body Battery, activities/gear/device/PR tables; activity rows link to the Activity dashboard
- **Garmin Activity** — per-activity deep dive: route map (colored by heart rate), heart rate/speed and elevation/power charts with a linked crosshair, splits, time in HR zones; selecting an activity automatically fits the time range to it
- **Garmin Cycling** — per-ride deep dive: route map, power meter chart, heart rate/speed, elevation, time in power and HR zones, power distribution, splits
- **Garmin Health** — wellness trends: sleep stages, stress, HRV, SpO2, hydration, body composition
- **Garmin Fitness** — long-term fitness trends: VO2max, endurance/hill score, running tolerance, lactate threshold, race predictions, FTP, personal records
- **Garmin Athlete Comparison** — two athletes head-to-head over the last 30 days: mirrored training totals, overlaid distance/steps/resting HR/VO2max, side-by-side activity maps and tables; pick any two Garmin Connect data sources in the **Athlete A/B** dropdowns

## Multiple athletes

Add one Garmin Connect data source per athlete, each with its own credentials — instances are fully isolated (separate logins, caches, and token files). The bundled dashboards have a **Datasource** dropdown, so switching athlete is one click. The bundled **Garmin Athlete Comparison** dashboard puts two athletes head-to-head; for custom panels, use Grafana's built-in *Mixed* data source with one query per athlete.

If you use token files, give each athlete's data source **its own file path** — sharing a path would make one athlete resume the other's session.

## Notes & troubleshooting

- Responses are cached in the plugin (5 minutes for recent data, 24 hours for historical), so dashboard refreshes and alert rules do not hammer Garmin's API.
- Backend logs are prefixed `plugin.barnesc-garminconnect-datasource`; enable debug logging to see individual Garmin requests.
- Some panels can be legitimately empty: splits require laps (auto-lap or manual), FTP requires a power meter, weight requires weigh-ins.
- Metrics that need one Garmin request per day (sleep, stress, HRV, SpO2, …) are clamped to the most recent 93 days on longer ranges, with a panel notice; other metrics fetch the full range in chunks.
- This plugin uses Garmin Connect's unofficial web API via [go-garminconnect](https://github.com/barnes-c/go-garminconnect). It may break if Garmin changes their API. This project is not affiliated with, endorsed by, or connected to Garmin Ltd.
