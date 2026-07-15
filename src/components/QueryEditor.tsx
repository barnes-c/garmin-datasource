import React, { ChangeEvent } from 'react';
import { Combobox, ComboboxOption, InlineField, InlineSwitch, Input, Stack } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { MyDataSourceOptions, MyQuery, QueryType } from '../types';

type Props = QueryEditorProps<DataSource, MyQuery, MyDataSourceOptions>;

const queryTypes: Array<ComboboxOption<QueryType>> = [
  { label: 'Activities', value: 'activities', description: 'Activities in the dashboard time range as a table' },
  { label: 'Sport totals', value: 'sport_totals', description: 'Distance, time and activity count per sport in the dashboard time range' },
  { label: 'Track', value: 'track', description: 'GPS trackpoints of one activity, for the Geomap route layer' },
  { label: 'Metric', value: 'metric', description: 'Health/training metric over the dashboard time range' },
  { label: 'Splits', value: 'splits', description: 'Per-split stats of one activity' },
  { label: 'Power', value: 'power', description: 'Power meter samples of one activity (W)' },
  { label: 'HR zones', value: 'hr_zones', description: 'Time in heart rate zones of one activity' },
  { label: 'Power zones', value: 'power_zones', description: 'Time in power zones of one activity' },
  { label: 'Gear', value: 'gear', description: 'Registered gear with lifetime usage' },
  { label: 'Devices', value: 'devices', description: 'Registered Garmin devices' },
  { label: 'Personal records', value: 'personal_records', description: 'Personal records table' },
  { label: 'HR zone settings', value: 'hr_zone_config', description: 'Configured heart rate zones per sport' },
  { label: 'Power zone settings', value: 'power_zone_config', description: 'Configured power zones and FTP per sport' },
];

const needsActivityId = (queryType?: string) =>
  queryType === 'track' || queryType === 'splits' || queryType === 'power' || queryType === 'hr_zones' || queryType === 'power_zones';

const metrics: Array<ComboboxOption<string>> = [
  { label: 'Body Battery', value: 'body_battery', description: 'Intraday Body Battery level' },
  { label: 'Blood pressure', value: 'blood_pressure', description: 'Systolic/diastolic/pulse readings' },
  { label: 'Body composition', value: 'body_composition', description: 'Weight, BMI, body fat, muscle mass, …' },
  { label: 'Cycling FTP', value: 'ftp', description: 'Latest functional threshold power (W)' },
  { label: 'Endurance score', value: 'endurance_score', description: 'Daily endurance score' },
  { label: 'Fitness age', value: 'fitness_age', description: 'Daily fitness age' },
  { label: 'Floors climbed', value: 'floors', description: 'Daily floors ascended' },
  { label: 'HRV', value: 'hrv', description: 'Nightly heart rate variability average (ms)' },
  { label: 'Hill score', value: 'hill_score', description: 'Daily hill score' },
  { label: 'Hydration', value: 'hydration', description: 'Daily water intake (ml)' },
  { label: 'Intensity minutes', value: 'intensity_minutes', description: 'Daily intensity minutes (vigorous counts double, like Garmin)' },
  { label: 'Lactate threshold', value: 'lactate_threshold', description: 'Latest LT speed and heart rates' },
  { label: 'Race predictions', value: 'race_predictions', description: 'Predicted 5k/10k/half/marathon times' },
  { label: 'Respiration', value: 'respiration', description: 'Daily average waking breaths/min' },
  { label: 'Resting heart rate', value: 'resting_heart_rate', description: 'Daily resting heart rate (bpm)' },
  { label: 'Running tolerance', value: 'running_tolerance', description: 'Daily running tolerance score' },
  { label: 'Sleep', value: 'sleep', description: 'Nightly total/deep/light/REM/awake durations' },
  { label: 'SpO2', value: 'spo2', description: 'Daily average blood oxygen (%)' },
  { label: 'Steps', value: 'steps', description: 'Daily step total' },
  { label: 'Stress', value: 'stress', description: 'Daily average stress level' },
  { label: 'Training readiness', value: 'training_readiness', description: 'Daily training readiness score' },
  { label: 'VO2max', value: 'vo2max', description: 'Daily VO2max estimate' },
  { label: 'Weight', value: 'weight', description: 'Weigh-ins (kg)' },
];

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const onQueryTypeChange = (value: ComboboxOption<QueryType> | null) => {
    onChange({ ...query, queryType: value?.value ?? 'activities' });
    onRunQuery();
  };

  const onActivityIdChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, activityId: event.target.value });
  };

  const onLimitChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, limit: parseInt(event.target.value, 10) || undefined });
  };

  const onMetricChange = (value: ComboboxOption<string> | null) => {
    onChange({ ...query, metric: value?.value });
    onRunQuery();
  };

  const onActivityTypeChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, activityType: event.target.value });
  };

  const onZoomChange = (event: ChangeEvent<HTMLInputElement>) => {
    onChange({ ...query, zoomToActivity: event.target.checked || undefined });
    onRunQuery();
  };

  const { queryType, activityId, activityType, limit, metric, zoomToActivity } = query;

  return (
    <Stack gap={0}>
      <InlineField label="Query type" labelWidth={14}>
        <Combobox
          id="query-editor-query-type"
          options={queryTypes}
          value={queryType}
          onChange={onQueryTypeChange}
          width={30}
        />
      </InlineField>
      {queryType === 'metric' && (
        <InlineField label="Metric" labelWidth={14} tooltip="Fetched over the dashboard time range">
          <Combobox id="query-editor-metric" options={metrics} value={metric} onChange={onMetricChange} width={30} />
        </InlineField>
      )}
      {needsActivityId(queryType) && (
        <InlineField
          label="Activity ID"
          labelWidth={14}
          tooltip="Garmin activity id; supports dashboard variables, e.g. $activity"
        >
          <Input
            id="query-editor-activity-id"
            onChange={onActivityIdChange}
            onBlur={onRunQuery}
            value={activityId || ''}
            required
            placeholder="12345678901 or $activity"
            width={30}
          />
        </InlineField>
      )}
      {(queryType === 'track' || queryType === 'power') && (
        <InlineField
          label="Fit time range"
          labelWidth={14}
          tooltip="When the activity changes, set the dashboard time range to its recording window"
        >
          <InlineSwitch id="query-editor-zoom" value={!!zoomToActivity} onChange={onZoomChange} />
        </InlineField>
      )}
      {(queryType === 'activities' || queryType === 'sport_totals') && (
        <InlineField label="Type" labelWidth={14} tooltip="Optional activity type filter, e.g. cycling or running">
          <Input
            id="query-editor-activity-type"
            onChange={onActivityTypeChange}
            onBlur={onRunQuery}
            value={activityType || ''}
            placeholder="all types"
            width={20}
          />
        </InlineField>
      )}
      {queryType === 'activities' && (
        <InlineField label="Limit" labelWidth={14} tooltip="Maximum number of activities (empty = all in range)">
          <Input
            id="query-editor-limit"
            onChange={onLimitChange}
            onBlur={onRunQuery}
            value={limit ?? ''}
            type="number"
            width={20}
          />
        </InlineField>
      )}
    </Stack>
  );
}
