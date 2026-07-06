import React from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryEditor } from './QueryEditor';
import { MyQuery } from '../types';
import { DataSource } from '../datasource';

const renderEditor = (query: Partial<MyQuery>, onChange = jest.fn(), onRunQuery = jest.fn()) => {
  render(
    <QueryEditor
      query={{ refId: 'A', queryType: 'activities', ...query } as MyQuery}
      onChange={onChange}
      onRunQuery={onRunQuery}
      datasource={{} as DataSource}
    />
  );
  return { onChange, onRunQuery };
};

describe('QueryEditor', () => {
  it('shows type filter and limit for activities queries', () => {
    renderEditor({ queryType: 'activities' });
    expect(screen.getByPlaceholderText('all types')).toBeInTheDocument();
    expect(screen.getByLabelText('Limit')).toBeInTheDocument();
  });

  it('shows the activity id field for track queries', () => {
    renderEditor({ queryType: 'track' });
    expect(screen.getByPlaceholderText('12345678901 or $activity')).toBeInTheDocument();
    expect(screen.queryByPlaceholderText('all types')).not.toBeInTheDocument();
  });

  it('shows the metric picker for metric queries', () => {
    renderEditor({ queryType: 'metric' });
    expect(screen.getByLabelText('Metric')).toBeInTheDocument();
  });

  it('propagates activity id changes', async () => {
    const { onChange } = renderEditor({ queryType: 'track' });
    await userEvent.type(screen.getByPlaceholderText('12345678901 or $activity'), '4');
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ activityId: '4' }));
  });
});
