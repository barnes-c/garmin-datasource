import { test, expect, PanelEditPage } from '@grafana/plugin-e2e';
import { Page, Locator } from '@playwright/test';

async function selectQueryType(page: Page, row: Locator, label: string) {
  const combo = row.getByRole('combobox').first();
  await combo.click();
  await combo.fill(label);
  // Click the option rather than pressing Enter: Enter races the async
  // option list and can leave the previous selection in place. Option
  // accessible names start with the label, followed by the description.
  await page
    .getByRole('option', { name: new RegExp(`^${label}`) })
    .first()
    .click();
}

async function editorRow(panelEditPage: PanelEditPage) {
  return panelEditPage.getQueryEditorRow('A');
}

test('should render the query type picker', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  await expect((await editorRow(panelEditPage)).getByRole('combobox').first()).toBeVisible();
});

test('track query type should show the activity id field', async ({ panelEditPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = await editorRow(panelEditPage);
  await selectQueryType(page, row, 'Track');
  await expect(row.getByPlaceholder('12345678901 or $activity')).toBeVisible();
});

test('metric query type should show the metric picker', async ({ panelEditPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = await editorRow(panelEditPage);
  await selectQueryType(page, row, 'Metric');
  await expect(row.getByRole('combobox').nth(1)).toBeVisible();
});
