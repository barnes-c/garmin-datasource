import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type QueryType =
  | 'activities'
  | 'sport_totals'
  | 'track'
  | 'metric'
  | 'gear'
  | 'devices'
  | 'personal_records'
  | 'splits'
  | 'power'
  | 'hr_zones'
  | 'power_zones'
  | 'hr_zone_config'
  | 'power_zone_config';

export type SpeedUnit = 'kmh' | 'mph' | 'ms';
export type UnitSystem = 'metric' | 'imperial';

export interface MyQuery extends DataQuery {
  queryType: QueryType;
  activityId?: string;
  activityType?: string;
  limit?: number;
  metric?: string;
  /** Fit the dashboard time range to this activity's recording window when the activity changes */
  zoomToActivity?: boolean;
}

export const DEFAULT_QUERY: Partial<MyQuery> = {
  queryType: 'activities',
  limit: 50,
};

export interface MyDataSourceOptions extends DataSourceJsonData {
  email?: string;
  speedUnit?: SpeedUnit;
  unitSystem?: UnitSystem;
}

/**
 * Value that is used in the backend, but never sent over HTTP to the frontend
 */
export interface MySecureJsonData {
  password?: string;
  /** Garmin OAuth token JSON; lets logins resume a session across restarts */
  token?: string;
}
