import { CustomVariableSupport, DataQueryRequest, DataQueryResponse, MetricFindValue, dateTime } from '@grafana/data';
import { Observable, map } from 'rxjs';

import { VariableQueryEditor } from './components/VariableQueryEditor';
import { DataSource } from './datasource';
import { MyQuery } from './types';

/**
 * Lists activities of the last year as variable options (text: "name (date)",
 * value: activity id), for use with track/splits/hr_zones queries. The list is
 * deliberately independent of the dashboard time range: per-activity
 * dashboards zoom to the selected activity's window, which would otherwise
 * shrink the picker to that single activity.
 */
export class GarminVariableSupport extends CustomVariableSupport<DataSource, MyQuery> {
  editor = VariableQueryEditor;

  constructor(private datasource: DataSource) {
    super();
  }

  query(request: DataQueryRequest<MyQuery>): Observable<DataQueryResponse> {
    const target: MyQuery = { ...request.targets[0], queryType: 'activities', refId: 'variable' };
    const to = dateTime();
    const from = dateTime(to.valueOf() - 365 * 24 * 3600 * 1000);
    const range = { from, to, raw: { from: 'now-1y', to: 'now' } };
    return this.datasource.query({ ...request, range, targets: [target] }).pipe(
      map((response) => {
        const frame = response.data?.[0];
        const values: MetricFindValue[] = [];
        if (frame) {
          const field = (name: string) => frame.fields.find((f: { name: string }) => f.name === name)?.values;
          const ids = field('id') ?? [];
          const names = field('name') ?? [];
          const times = field('time') ?? [];
          for (let i = 0; i < ids.length; i++) {
            const date = times[i] ? new Date(times[i]).toISOString().slice(0, 10) : '';
            values.push({ text: `${names[i]} (${date})`, value: String(ids[i]) });
          }
        }
        return { data: values };
      })
    );
  }
}
