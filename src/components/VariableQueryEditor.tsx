import React, { ChangeEvent } from 'react';
import { InlineField, Input, Stack } from '@grafana/ui';
import { MyQuery } from '../types';

interface Props {
  query: MyQuery;
  onChange: (query: MyQuery, definition?: string) => void;
}

export function VariableQueryEditor({ query, onChange }: Props) {
  const update = (patch: Partial<MyQuery>) => {
    const next = { ...query, ...patch };
    onChange(next, `activities${next.activityType ? ` (${next.activityType})` : ''}`);
  };

  return (
    <Stack gap={0}>
      <InlineField label="Activity type" labelWidth={16} tooltip="Optional filter, e.g. cycling or running">
        <Input
          id="variable-editor-activity-type"
          onChange={(event: ChangeEvent<HTMLInputElement>) => update({ activityType: event.target.value })}
          value={query.activityType || ''}
          placeholder="all types"
          width={20}
        />
      </InlineField>
      <InlineField label="Limit" labelWidth={16} tooltip="Maximum number of activities to list">
        <Input
          id="variable-editor-limit"
          onChange={(event: ChangeEvent<HTMLInputElement>) => update({ limit: parseInt(event.target.value, 10) || undefined })}
          value={query.limit ?? ''}
          type="number"
          width={20}
        />
      </InlineField>
    </Stack>
  );
}
