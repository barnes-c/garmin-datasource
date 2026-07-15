import React, { ChangeEvent, useState } from 'react';
import { Alert, Button, Combobox, ComboboxOption, FieldSet, InlineField, Input, SecretInput, Stack } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { MyDataSourceOptions, MySecureJsonData, SpeedUnit, UnitSystem } from '../types';

const speedUnits: Array<ComboboxOption<SpeedUnit>> = [
  { label: 'km/h', value: 'kmh' },
  { label: 'mph', value: 'mph' },
  { label: 'm/s', value: 'ms' },
];

const unitSystems: Array<ComboboxOption<UnitSystem>> = [
  { label: 'Metric (km / m / kg)', value: 'metric' },
  { label: 'Imperial (mi / ft / lbs)', value: 'imperial' },
];

interface Props extends DataSourcePluginOptionsEditorProps<MyDataSourceOptions, MySecureJsonData> {}

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const [mfaCode, setMfaCode] = useState('');
  const [verifying, setVerifying] = useState(false);
  const [mfaResult, setMfaResult] = useState<{ ok: boolean; text: string }>();
  const [fetchingToken, setFetchingToken] = useState(false);
  const [tokenResult, setTokenResult] = useState<{ ok: boolean; text: string }>();

  const onEmailChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        email: event.target.value,
      },
    });
  };

  const onPasswordChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        password: event.target.value,
      },
    });
  };

  const onPasswordReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        password: false,
      },
      secureJsonData: {
        ...secureJsonData,
        password: '',
      },
    });
  };

  const onTokenChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        token: event.target.value,
      },
    });
  };

  const onTokenReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        token: false,
      },
      secureJsonData: {
        ...secureJsonData,
        token: '',
      },
    });
  };

  // Stages the backend's current session token into the (unsaved) secure
  // settings; the backend cannot write secureJsonData itself.
  const onFetchToken = async () => {
    setFetchingToken(true);
    setTokenResult(undefined);
    try {
      const res = await getBackendSrv().get(`/api/datasources/uid/${options.uid}/resources/token`);
      onOptionsChange({
        ...options,
        secureJsonFields: {
          ...secureJsonFields,
          token: false,
        },
        secureJsonData: {
          ...secureJsonData,
          token: JSON.stringify(res),
        },
      });
      setTokenResult({ ok: true, text: 'Session token staged — click Save & test to persist it' });
    } catch (err) {
      const message =
        err && typeof err === 'object' && 'data' in err ? (err.data as { message?: string })?.message : undefined;
      setTokenResult({ ok: false, text: message ?? 'Could not fetch the session token' });
    } finally {
      setFetchingToken(false);
    }
  };

  const onVerifyMfa = async () => {
    setVerifying(true);
    setMfaResult(undefined);
    try {
      const res = await getBackendSrv().post(`/api/datasources/uid/${options.uid}/resources/mfa`, { code: mfaCode });
      setMfaResult({ ok: true, text: res?.message ?? 'MFA verified' });
      setMfaCode('');
    } catch (err) {
      const message =
        err && typeof err === 'object' && 'data' in err ? (err.data as { message?: string })?.message : undefined;
      setMfaResult({ ok: false, text: message ?? 'MFA verification failed' });
    } finally {
      setVerifying(false);
    }
  };

  return (
    <>
      <FieldSet label="Authentication">
        <InlineField label="Email" labelWidth={14} interactive tooltip={'Garmin Connect account email'}>
          <Input
            id="config-editor-email"
            onChange={onEmailChange}
            value={jsonData.email}
            placeholder="athlete@example.com"
            width={40}
          />
        </InlineField>
        <InlineField label="Password" labelWidth={14} interactive tooltip={'Garmin Connect account password'}>
          <SecretInput
            required
            id="config-editor-password"
            isConfigured={secureJsonFields.password}
            value={secureJsonData?.password}
            placeholder="Enter your password"
            width={40}
            onReset={onPasswordReset}
            onChange={onPasswordChange}
          />
        </InlineField>
        <Stack direction="row" alignItems="flex-start">
          <InlineField
            label="Session token"
            labelWidth={14}
            interactive
            tooltip={
              "Optional Garmin OAuth token (JSON, same format as garmin_exporter's token file). Lets logins resume the session instead of a fresh SSO login (and MFA code) after every Grafana restart. Click 'Load from session' after a successful Save & test, or paste a token for headless provisioning."
            }
          >
            <SecretInput
              id="config-editor-token"
              isConfigured={secureJsonFields.token}
              value={secureJsonData?.token}
              placeholder='{"access_token":"…"}'
              width={40}
              onReset={onTokenReset}
              onChange={onTokenChange}
            />
          </InlineField>
          <Button onClick={onFetchToken} disabled={fetchingToken} variant="secondary">
            {fetchingToken ? 'Loading…' : 'Load from session'}
          </Button>
        </Stack>
        {tokenResult && <Alert severity={tokenResult.ok ? 'success' : 'error'} title={tokenResult.text} />}
        <Stack direction="row" alignItems="flex-start">
          <InlineField
            label="MFA code"
            labelWidth={14}
            interactive
            tooltip={
              'Only needed when Save & test reports that Garmin sent a code to your email. Enter the code and click Verify to complete that login.'
            }
          >
            <Input
              id="config-editor-mfa-code"
              onChange={(event: ChangeEvent<HTMLInputElement>) => setMfaCode(event.target.value)}
              value={mfaCode}
              placeholder="123456"
              width={20}
            />
          </InlineField>
          <Button onClick={onVerifyMfa} disabled={!mfaCode || verifying} variant="secondary">
            {verifying ? 'Verifying…' : 'Verify'}
          </Button>
        </Stack>
        {mfaResult && <Alert severity={mfaResult.ok ? 'success' : 'error'} title={mfaResult.text} />}
      </FieldSet>
      <FieldSet label="Display">
        <InlineField
          label="Unit system"
          labelWidth={14}
          tooltip={'Distances, elevation and weight. Metric distances auto-scale between m and km; imperial shows miles, feet and pounds'}
        >
          <Combobox
            id="config-editor-unit-system"
            options={unitSystems}
            value={jsonData.unitSystem ?? 'metric'}
            onChange={(value: ComboboxOption<UnitSystem> | null) =>
              onOptionsChange({ ...options, jsonData: { ...jsonData, unitSystem: value?.value ?? 'metric' } })
            }
            width={24}
          />
        </InlineField>
        <InlineField label="Speed unit" labelWidth={14} tooltip={'Display unit for all speed values'}>
          <Combobox
            id="config-editor-speed-unit"
            options={speedUnits}
            value={jsonData.speedUnit ?? 'kmh'}
            onChange={(value: ComboboxOption<SpeedUnit> | null) =>
              onOptionsChange({ ...options, jsonData: { ...jsonData, speedUnit: value?.value ?? 'kmh' } })
            }
            width={20}
          />
        </InlineField>
      </FieldSet>
    </>
  );
}
