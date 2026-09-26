import { expect, test, type Locator, type Page } from '@playwright/test';
import { FIXTURE } from './fixture';

/**
 * Regression guard for the DEFAULT deployment while fixing GitHub issue #255
 * ("反代用域名访问打开商店空白").
 *
 * #255 is about the store being reverse-proxied under a sub-path
 * (https://domain.tld/store/). The fix routes every request through
 * `apiUrl()`, which derives the mount point from the bundle's own URL. That
 * derivation must be a no-op at the root mount — the LAN-IP deployment that
 * always worked — so this file proves the plain `/` case still renders, still
 * talks to `/api/...` (never `//api/...`, never a doubled prefix) and never
 * takes a 4xx/5xx on an API call. A blank page was exactly what #255 reported.
 *
 * Selectors and fixture data are shared with uninstall.spec.ts (issue #265).
 */

/**
 * The card root is the nearest `overflow-hidden` ancestor of the app's title,
 * scoped to <main> because the detail dialog portals a same-named heading.
 */
function cardFor(page: Page, displayName: string): Locator {
  return page
    .locator('main')
    .getByRole('heading', { name: displayName, exact: true })
    .locator('xpath=ancestor::div[contains(@class,"overflow-hidden")][1]');
}

/** Land on the root mount and wait until the app list has really rendered. */
async function gotoRoot(page: Page): Promise<void> {
  await page.goto('/');
  await expect(cardFor(page, FIXTURE.keepApp.displayName)).toBeVisible({ timeout: 30_000 });
}

/**
 * True for a real backend call.
 *
 * Deliberately matches '/api/' ANYWHERE in the path, so a wrongly prefixed
 * '/store/api/apps' is still collected and then fails the assertions below.
 * Vite's dev server also serves the module graph over HTTP — including a module
 * literally named /src/api/client.ts — so its internal namespaces are excluded.
 */
function isApiCall(pathname: string): boolean {
  if (pathname.startsWith('/src/')) return false;
  if (pathname.startsWith('/node_modules/')) return false;
  if (pathname.startsWith('/@')) return false;
  return pathname.includes('/api/');
}

test('root-mount-renders-and-calls-unprefixed-api', async ({ page }) => {
  const apiRequests: string[] = [];
  const apiFailures: string[] = [];

  page.on('request', (req) => {
    const { pathname } = new URL(req.url());
    if (isApiCall(pathname)) apiRequests.push(pathname);
  });
  page.on('response', (res) => {
    const { pathname } = new URL(res.url());
    if (isApiCall(pathname) && res.status() >= 400) {
      apiFailures.push(`${res.status()} ${pathname}`);
    }
  });

  await gotoRoot(page);

  // Not a blank page: the list rendered real data from the backend.
  await expect(page.locator('#root')).not.toBeEmpty();
  await expect(cardFor(page, FIXTURE.notInstalledApp.displayName)).toBeVisible();

  // The app really did talk to the API (otherwise the assertions below are vacuous).
  expect(apiRequests.length).toBeGreaterThan(0);

  for (const pathname of apiRequests) {
    // At the root mount the resolver must add nothing: no '/store/api/...',
    // and no '//api/...' (which a browser reads as a foreign host).
    expect(pathname, `unexpected api path ${pathname}`).toMatch(/^\/api\//);
    expect(pathname, `double slash in ${pathname}`).not.toContain('//');
  }

  // #255's symptom was 404s on every API call; there must be none here.
  expect(apiFailures).toEqual([]);
});

test('root-mount-sse-operation-url', async ({ page }) => {
  const { appname, displayName } = FIXTURE.keepApp;
  let requested: string | undefined;

  // Capture the streamSSE target without ever letting it reach the backend, so
  // no server state is mutated (same technique as uninstall.spec.ts).
  await page.route(`**/api/apps/*/update`, (route) => {
    requested = new URL(route.request().url()).pathname;
    /* intentionally never fulfilled */
  });

  await gotoRoot(page);
  await cardFor(page, displayName).getByRole('button', { name: '更新', exact: true }).click();

  await expect.poll(() => requested).toBe(`/api/apps/${appname}/update`);
});

test('root-mount-download-link-href', async ({ page }) => {
  const { appname, displayName } = FIXTURE.keepApp;

  await gotoRoot(page);
  await cardFor(page, displayName)
    .getByRole('heading', { name: displayName, exact: true })
    .click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  // The href is built with apiUrl(), so at the root mount it must stay exactly
  // as it was before the fix.
  await expect(dialog.getByRole('link', { name: /下载 fpk/ })).toHaveAttribute(
    'href',
    `/api/apps/${appname}/download`,
  );
});

test('root-mount-favicon-resolves', async ({ page }) => {
  await gotoRoot(page);

  const href = await page.locator('link[rel="icon"]').getAttribute('href');
  expect(href).not.toBeNull();

  // index.html declares it mount-relative ('./vite.svg') so it follows the app
  // under a proxy prefix; at the root mount it must still be reachable.
  const resolved = new URL(href ?? '', page.url());
  expect(resolved.origin).toBe(new URL(page.url()).origin);
  expect(resolved.pathname).toBe('/vite.svg');

  const res = await page.request.get(resolved.toString());
  expect(res.status()).toBe(200);
});
