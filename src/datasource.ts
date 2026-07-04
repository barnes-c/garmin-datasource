import { DataSourceInstanceSettings, CoreApp, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { MyQuery, MyDataSourceOptions, DEFAULT_QUERY } from './types';
import { GarminVariableSupport } from './variables';

export class DataSource extends DataSourceWithBackend<MyQuery, MyDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<MyDataSourceOptions>) {
    super(instanceSettings);
    this.variables = new GarminVariableSupport(this);
  }

  getDefaultQuery(_: CoreApp): Partial<MyQuery> {
    return DEFAULT_QUERY;
  }

  applyTemplateVariables(query: MyQuery, scopedVars: ScopedVars) {
    return {
      ...query,
      activityId: getTemplateSrv().replace(query.activityId, scopedVars),
      activityType: getTemplateSrv().replace(query.activityType, scopedVars),
    };
  }

  filterQuery(query: MyQuery): boolean {
    switch (query.queryType) {
      case 'track':
      case 'splits':
      case 'hr_zones':
        return !!query.activityId;
      case 'metric':
        return !!query.metric;
      default:
        return true;
    }
  }
}
