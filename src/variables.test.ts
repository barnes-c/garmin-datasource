import { DataQueryRequest, DataQueryResponse, MetricFindValue } from '@grafana/data';
import { firstValueFrom, of } from 'rxjs';
import { GarminVariableSupport } from './variables';
import { DataSource } from './datasource';
import { MyQuery } from './types';

describe('GarminVariableSupport', () => {
  it('maps the activities frame to picker options', async () => {
    const frame = {
      fields: [
        { name: 'id', values: [11, 22] },
        { name: 'name', values: ['Morning Ride', 'Evening Run'] },
        { name: 'time', values: [Date.UTC(2026, 6, 1), Date.UTC(2026, 6, 2)] },
      ],
    };
    const datasource = {
      query: () => of({ data: [frame] } as DataQueryResponse),
    } as unknown as DataSource;

    const support = new GarminVariableSupport(datasource);
    const request = { targets: [{ refId: 'variable' } as MyQuery] } as DataQueryRequest<MyQuery>;

    const response = await firstValueFrom(support.query(request));
    const values = response.data as MetricFindValue[];
    expect(values).toEqual([
      { text: 'Morning Ride (2026-07-01)', value: '11' },
      { text: 'Evening Run (2026-07-02)', value: '22' },
    ]);
  });

  it('returns no options when the query fails to produce a frame', async () => {
    const datasource = { query: () => of({ data: [] } as DataQueryResponse) } as unknown as DataSource;
    const support = new GarminVariableSupport(datasource);
    const request = { targets: [{ refId: 'variable' } as MyQuery] } as DataQueryRequest<MyQuery>;

    const response = await firstValueFrom(support.query(request));
    expect(response.data).toEqual([]);
  });
});
