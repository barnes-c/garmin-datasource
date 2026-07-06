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

  const onEmailChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        email: event.target.value,
      },
    });
  };

  const onTokenFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        tokenFile: event.target.value,
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
        <InlineField
          label="Token file"
          labelWidth={14}
          interactive
          tooltip={
            "Optional writable path where the Garmin OAuth token is cached (same format as garmin_exporter's --token-file). Avoids a fresh login (and MFA code) after every Grafana restart. Leave empty to keep tokens in memory only. When configuring multiple athletes, give each datasource its own file."
          }
        >
          <Input
            id="config-editor-token-file"
            onChange={onTokenFileChange}
            value={jsonData.tokenFile}
            placeholder="/var/lib/grafana/garmin_token.json"
            width={40}
          />
        </InlineField>
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
