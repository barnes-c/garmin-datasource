import { test, expect } from '@grafana/plugin-e2e';

test('should render all configuration fields', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByPlaceholder('athlete@example.com')).toBeVisible();
  await expect(page.getByPlaceholder('Enter your password')).toBeVisible();
  await expect(page.getByPlaceholder('/var/lib/grafana/garmin_token.json')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Verify' })).toBeVisible();
});

test('"Save & test" should fail without credentials', async ({ createDataSourceConfigPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'Email and password are required' });
});
