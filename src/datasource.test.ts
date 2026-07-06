import { DataSourceInstanceSettings } from '@grafana/data';
import { DataSource } from './datasource';
import { MyDataSourceOptions, MyQuery } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (value?: string) => value?.replace('$activity', '42'),
  }),
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
