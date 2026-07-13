import { DataSourceInstanceSettings, CoreApp, DataFrame, DataQueryRequest, DataQueryResponse, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv, locationService } from '@grafana/runtime';
import { Observable, tap } from 'rxjs';

import { MyQuery, MyDataSourceOptions, DEFAULT_QUERY } from './types';
import { GarminVariableSupport } from './variables';

export class DataSource extends DataSourceWithBackend<MyQuery, MyDataSourceOptions> {
  private zoomedActivity?: string;

  constructor(instanceSettings: DataSourceInstanceSettings<MyDataSourceOptions>) {
    super(instanceSettings);
    this.variables = new GarminVariableSupport(this);
  }

  getDefaultQuery(_: CoreApp): Partial<MyQuery> {
    return DEFAULT_QUERY;
  }

  query(request: DataQueryRequest<MyQuery>): Observable<DataQueryResponse> {
    return super.query(request).pipe(tap((response) => this.zoomToActivity(request, response)));
  }

  /**
   * Fits the dashboard time range to the activity's recording window whenever
   * the activity of a target flagged with zoomToActivity changes. Per-activity
   * charts use time on the x-axis, so without this the selected activity would
   * be a sliver (or outside) of the dashboard time range.
   */
  private zoomToActivity(request: DataQueryRequest<MyQuery>, response: DataQueryResponse) {
    for (const target of request.targets) {
      if (!target.zoomToActivity || !target.activityId) {
        continue;
      }
      const activityId = getTemplateSrv().replace(target.activityId, request.scopedVars);
      if (!activityId || activityId === this.zoomedActivity) {
        continue;
      }
      const frame: DataFrame | undefined = response.data?.find((d: DataFrame) => d.refId === target.refId);
      const times = frame?.fields?.find((f) => f.name === 'time')?.values;
      if (!times?.length) {
        continue;
      }
      this.zoomedActivity = activityId;
      const from = times[0];
      const to = times[times.length - 1];
      // Already fitted (e.g. arriving through a deep link) — avoid a pointless refresh.
      if (Math.abs(request.range.from.valueOf() - from) > 1000 || Math.abs(request.range.to.valueOf() - to) > 1000) {
        locationService.partial({ from, to }, true);
      }
      return;
    }
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
      case 'power':
      case 'hr_zones':
      case 'power_zones':
        return !!query.activityId;
      case 'metric':
        return !!query.metric;
      default:
        return true;
    }
  }
}
