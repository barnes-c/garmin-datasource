import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export type QueryType =
  | 'activities'
  | 'track'
  | 'metric'
  | 'gear'
  | 'devices'
  | 'personal_records'
  | 'splits'
  | 'hr_zones';

export interface MyQuery extends DataQuery {
  queryType: QueryType;
  activityId?: string;
  activityType?: string;
  limit?: number;
  metric?: string;
}

export const DEFAULT_QUERY: Partial<MyQuery> = {
  queryType: 'activities',
  limit: 50,
};

export interface MyDataSourceOptions extends DataSourceJsonData {
  email?: string;
  tokenFile?: string;
}

/**
 * Value that is used in the backend, but never sent over HTTP to the frontend
 */
export interface MySecureJsonData {
  password?: string;
  mfaCode?: string;
}
