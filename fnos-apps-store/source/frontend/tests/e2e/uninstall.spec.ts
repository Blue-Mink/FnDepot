import { expect, test, type Locator, type Page } from '@playwright/test';
import { FIXTURE, resetMockApps } from './fixture';

/**
 * Regression coverage for GitHub issue #265 — an app installed by the store
 * could never be uninstalled from the store, because the uninstall control was
 * never rendered (the whole backend + App.tsx pipeline already existed).
 *
 * App names come from dev/mock-apps: Jellyfin / qBittorrent / Plex are
 * installed, Transmission is not.
 */

/**
 * The card root is the nearest `overflow-hidden` ancestor of the app's title.
 * Scoped to <main> because the detail dialog renders its title in a portal
 * outside <main> with the same accessible name.
 */
function cardFor(page: Page, displayName: string): Locator {
  return page
    .locator('main')
    .getByRole('heading', { name: displayName, exact: true })
    .locator('xpath=ancestor::div[contains(@class,"overflow-hidden")][1]');
}

/** The uninstall control, wherever it is rendered. */
function uninstallControl(scope: Locator | Page): Locator {
  return scope.getByRole('button', { name: /卸载/ });
}

async function openDetailDialog(page: Page, displayName: string): Promise<Locator> {
  await cardFor(page, displayName)
    .getByRole('heading', { name: displayName, exact: true })
    .click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  return dialog;
}

/** Click 确认卸载 in the confirm AlertDialog (App.tsx:1146-1161). */
async function confirmUninstall(page: Page, displayName: string): Promise<void> {
  const alert = page.getByRole('alertdialog');
  await expect(alert).toBeVisible();
  await expect(alert).toContainText(`确定要卸载 ${displayName} 吗？此操作无法撤销。`);
  await alert.getByRole('button', { name: '确认卸载', exact: true }).click();
}

const API = 'http://localhost:38011';

interface ApiApp {
  appname: string;
  installed: boolean;
}

async function installedAppnames(): Promise<Set<string>> {
  const res = await fetch(`${API}/api/apps`);
  if (!res.ok) return new Set();
  const body = (await res.json()) as { apps?: ApiApp[] };
  return new Set((body.apps ?? []).filter((a) => a.installed).map((a) => a.appname));
}

/**
 * Playwright starts `webServer` BEFORE globalSetup, so the backend can boot
 * against a fixture a previous run already consumed by uninstalling from it.
 * Restore the fixture and force the registry to re-scan APPS_DIR (POST
 * /api/check -> refreshRegistry -> core.ScanInstalled) before any assertion.
 * This makes the suite repeatable instead of pass-once.
 */
test.beforeAll(async () => {
  resetMockApps();
  const expected = [FIXTURE.cardApp, FIXTURE.dialogApp, FIXTURE.keepApp].map((a) => a.appname);
  const deadline = Date.now() + 90_000;
  for (;;) {
    await fetch(`${API}/api/check`, { method: 'POST' }).catch(() => undefined);
    const installed = await installedAppnames();
    if (expected.every((name) => installed.has(name))) return;
    if (Date.now() > deadline) {
      throw new Error(`fixture not restored; installed = ${[...installed].join(', ')}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
});

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  // The list is fetched after mount; wait for a known app before asserting.
  await expect(cardFor(page, FIXTURE.keepApp.displayName)).toBeVisible({ timeout: 30_000 });
});

test('uninstall-from-card', async ({ page }) => {
  const { displayName } = FIXTURE.cardApp;
  const card = cardFor(page, displayName);

  const uninstall = uninstallControl(card);
  await expect(uninstall).toBeVisible();
  await expect(uninstall).toBeEnabled();
  await uninstall.click();

  await confirmUninstall(page, displayName);

  // Uninstall really happened: the card offers 安装 again.
  await expect(card.getByRole('button', { name: '安装', exact: true })).toBeVisible({
    timeout: 60_000,
  });
  await expect(uninstallControl(card)).toHaveCount(0);
});

test('uninstall-from-detail-dialog', async ({ page }) => {
  const { displayName } = FIXTURE.dialogApp;

  const dialog = await openDetailDialog(page, displayName);
  const uninstall = uninstallControl(dialog);
  await expect(uninstall).toBeVisible();
  await expect(uninstall).toBeEnabled();
  await uninstall.click();

  await confirmUninstall(page, displayName);

  await expect(
    cardFor(page, displayName).getByRole('button', { name: '安装', exact: true }),
  ).toBeVisible({ timeout: 60_000 });
});

test('uninstall-inflight-disabled', async ({ page }) => {
  const { appname, displayName } = FIXTURE.keepApp;

  // Stall the update stream so an operation stays in flight for this app.
  // The request never reaches the backend, so no server state is touched.
  await page.route(`**/api/apps/${appname}/update`, () => {
    /* intentionally never fulfilled */
  });

  const card = cardFor(page, displayName);
  await card.getByRole('button', { name: '更新', exact: true }).click();

  // Card swaps its action row for the progress row — nothing to click there.
  await expect(card.getByRole('progressbar')).toBeVisible();
  await expect(uninstallControl(card)).toHaveCount(0);

  // The detail dialog still offers the control, but it must not be actionable.
  const dialog = await openDetailDialog(page, displayName);
  const uninstall = uninstallControl(dialog);
  await expect(uninstall).toBeVisible();
  await expect(uninstall).toBeDisabled();
});

test('no-uninstall-when-not-installed', async ({ page }) => {
  const card = cardFor(page, FIXTURE.notInstalledApp.displayName);
  await expect(card).toBeVisible();
  await expect(card).toContainText('未安装');
  await expect(uninstallControl(card)).toHaveCount(0);
});

test('install-button-regression', async ({ page }) => {
  const notInstalled = cardFor(page, FIXTURE.notInstalledApp.displayName);
  await expect(notInstalled.getByRole('button', { name: '安装', exact: true })).toBeVisible();
  await expect(notInstalled.getByRole('button', { name: '更新', exact: true })).toHaveCount(0);

  const installed = cardFor(page, FIXTURE.keepApp.displayName);
  await expect(installed.getByRole('button', { name: '更新', exact: true })).toBeVisible();
  await expect(installed.getByRole('button', { name: '安装', exact: true })).toHaveCount(0);

  // Detail dialog keeps its existing actions.
  const dialog = await openDetailDialog(page, FIXTURE.keepApp.displayName);
  await expect(dialog.getByRole('button', { name: '更新', exact: true })).toBeVisible();
  await expect(dialog.getByRole('link', { name: /下载 fpk/ })).toBeVisible();
});
