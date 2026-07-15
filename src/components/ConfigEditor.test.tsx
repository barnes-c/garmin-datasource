import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfigEditor } from './ConfigEditor';
import { MyDataSourceOptions, MySecureJsonData } from '../types';
import { DataSourceSettings } from '@grafana/data';

const mockPost = jest.fn();
const mockGet = jest.fn();
jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getBackendSrv: () => ({ post: mockPost, get: mockGet }),
}));

const options = {
  uid: 'test-uid',
  jsonData: {},
  secureJsonFields: {},
  secureJsonData: {},
} as unknown as DataSourceSettings<MyDataSourceOptions, MySecureJsonData>;

describe('ConfigEditor', () => {
  beforeEach(() => {
    mockPost.mockReset();
    mockGet.mockReset();
  });

  it('renders authentication and display fields', () => {
    render(<ConfigEditor options={options} onOptionsChange={jest.fn()} />);
    expect(screen.getByPlaceholderText('athlete@example.com')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Enter your password')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('{"access_token":"…"}')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Load from session' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Verify' })).toBeDisabled();
    expect(screen.getByText('Authentication')).toBeInTheDocument();
    expect(screen.getByText('Display')).toBeInTheDocument();
  });

  it('stages the session token from the resource endpoint', async () => {
    mockGet.mockResolvedValue({ access_token: 'a1', refresh_token: 'r1' });
    const onOptionsChange = jest.fn();
    render(<ConfigEditor options={options} onOptionsChange={onOptionsChange} />);

    await userEvent.click(screen.getByRole('button', { name: 'Load from session' }));

    expect(mockGet).toHaveBeenCalledWith('/api/datasources/uid/test-uid/resources/token');
    await waitFor(() =>
      expect(onOptionsChange).toHaveBeenCalledWith(
        expect.objectContaining({
          secureJsonData: { token: JSON.stringify({ access_token: 'a1', refresh_token: 'r1' }) },
        })
      )
    );
    expect(screen.getByText('Session token staged — click Save & test to persist it')).toBeInTheDocument();
  });

  it('shows the backend error when the token fetch fails', async () => {
    mockGet.mockRejectedValue({ data: { message: 'not logged in; click Save & test first' } });
    render(<ConfigEditor options={options} onOptionsChange={jest.fn()} />);

    await userEvent.click(screen.getByRole('button', { name: 'Load from session' }));

    await waitFor(() => expect(screen.getByText('not logged in; click Save & test first')).toBeInTheDocument());
  });

  it('propagates email changes', async () => {
    const onOptionsChange = jest.fn();
    render(<ConfigEditor options={options} onOptionsChange={onOptionsChange} />);
    await userEvent.type(screen.getByPlaceholderText('athlete@example.com'), 'a');
    expect(onOptionsChange).toHaveBeenCalledWith(expect.objectContaining({ jsonData: { email: 'a' } }));
  });

  it('verifies the MFA code against the resource endpoint', async () => {
    mockPost.mockResolvedValue({ message: 'MFA verified — logged in to Garmin Connect' });
    render(<ConfigEditor options={options} onOptionsChange={jest.fn()} />);

    await userEvent.type(screen.getByPlaceholderText('123456'), '424242');
    await userEvent.click(screen.getByRole('button', { name: 'Verify' }));

    expect(mockPost).toHaveBeenCalledWith('/api/datasources/uid/test-uid/resources/mfa', { code: '424242' });
    await waitFor(() =>
      expect(screen.getByText('MFA verified — logged in to Garmin Connect')).toBeInTheDocument()
    );
  });

  it('shows the backend error when verification fails', async () => {
    mockPost.mockRejectedValue({ data: { message: 'no login is waiting for an MFA code; click Save & test first' } });
    render(<ConfigEditor options={options} onOptionsChange={jest.fn()} />);

    await userEvent.type(screen.getByPlaceholderText('123456'), '1');
    await userEvent.click(screen.getByRole('button', { name: 'Verify' }));

    await waitFor(() =>
      expect(screen.getByText('no login is waiting for an MFA code; click Save & test first')).toBeInTheDocument()
    );
  });
});
