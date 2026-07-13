import { DataQueryRequest, DataQueryResponse, DataSourceInstanceSettings, dateTime } from '@grafana/data';
import { locationService } from '@grafana/runtime';
import { DataSource } from './datasource';
import { MyDataSourceOptions, MyQuery } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (value?: string) => value?.replace('$activity', '42'),
  }),
  locationService: {
    partial: jest.fn(),
  },
}));

const ds = new DataSource({
  id: 1,
  uid: 'test',
  jsonData: {},
  access: 'proxy',
} as unknown as DataSourceInstanceSettings<MyDataSourceOptions>);

const query = (overrides: Partial<MyQuery>): MyQuery => ({ refId: 'A', queryType: 'activities', ...overrides });

describe('filterQuery', () => {
  it.each([
    [query({ queryType: 'track' }), false],
    [query({ queryType: 'track', activityId: '1' }), true],
    [query({ queryType: 'splits' }), false],
    [query({ queryType: 'hr_zones', activityId: '1' }), true],
    [query({ queryType: 'metric' }), false],
    [query({ queryType: 'metric', metric: 'steps' }), true],
    [query({ queryType: 'activities' }), true],
    [query({ queryType: 'gear' }), true],
  ])('%o → %s', (q, expected) => {
    expect(ds.filterQuery(q)).toBe(expected);
  });
});

describe('applyTemplateVariables', () => {
  it('replaces dashboard variables in the activity id', () => {
    const result = ds.applyTemplateVariables(query({ queryType: 'track', activityId: '$activity' }), {});
    expect(result.activityId).toBe('42');
  });
});

describe('zoomToActivity', () => {
  const start = Date.UTC(2026, 6, 12, 10);
  const end = Date.UTC(2026, 6, 12, 13);
  const request = (activityId: string, from = Date.UTC(2026, 4, 1), to = Date.UTC(2026, 7, 1)) =>
    ({
      targets: [query({ refId: 'A', queryType: 'track', activityId, zoomToActivity: true })],
      scopedVars: {},
      range: { from: dateTime(from), to: dateTime(to), raw: {} },
    }) as unknown as DataQueryRequest<MyQuery>;
  const response = {
    data: [{ refId: 'A', fields: [{ name: 'time', values: [start, end] }] }],
  } as unknown as DataQueryResponse;
  const zoom = (ds as unknown as { zoomToActivity: (r: DataQueryRequest<MyQuery>, x: DataQueryResponse) => void })
    .zoomToActivity;

  beforeEach(() => {
    jest.clearAllMocks();
    (ds as unknown as { zoomedActivity?: string }).zoomedActivity = undefined;
  });

  it('fits the time range to the activity when it changes', () => {
    zoom.call(ds, request('$activity'), response);
    expect(locationService.partial).toHaveBeenCalledWith({ from: start, to: end }, true);
  });

  it('does not refit for the same activity', () => {
    zoom.call(ds, request('$activity'), response);
    zoom.call(ds, request('$activity'), response);
    expect(locationService.partial).toHaveBeenCalledTimes(1);
  });

  it('skips navigation when the range is already fitted', () => {
    zoom.call(ds, request('$activity', start, end), response);
    expect(locationService.partial).not.toHaveBeenCalled();
  });

  it('ignores targets without the flag or without data', () => {
    const plain = request('$activity');
    plain.targets[0].zoomToActivity = false;
    zoom.call(ds, plain, response);
    zoom.call(ds, request('$activity'), { data: [] } as unknown as DataQueryResponse);
    expect(locationService.partial).not.toHaveBeenCalled();
  });
});
