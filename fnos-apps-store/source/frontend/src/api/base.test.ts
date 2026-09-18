import { describe, expect, it } from 'vitest';

import { resolveApiUrl } from './base';

/**
 * Regression coverage for GitHub issue #255 — "反代用域名访问打开商店空白".
 *
 * Behind a reverse proxy that mounts the store under a sub-path
 * (Lucky: `https://domain.tld/store/`) every root-absolute request escaped the
 * prefix and hit `https://domain.tld/api/...` instead of
 * `https://domain.tld/store/api/...`, so the SPA rendered blank while direct
 * LAN-IP access kept working.
 *
 * `resolveApiUrl` takes the mount point as a PARAMETER — never read from
 * `import.meta.url`, `location` or any other global — precisely so this matrix
 * of proxy layouts is testable without a browser, a bundler or a proxy.
 */

const APPS = '/api/apps';

/** Every mount point shape a reverse proxy can realistically hand us. */
const MOUNTS = [
  { name: 'root mount', base: '/', expected: '/api/apps' },
  { name: 'sub-path mount', base: '/store/', expected: '/store/api/apps' },
  { name: 'nested sub-path mount', base: '/a/b/', expected: '/a/b/api/apps' },
  { name: 'sub-path without trailing slash', base: '/store', expected: '/store/api/apps' },
  { name: 'nested sub-path without trailing slash', base: '/a/b', expected: '/a/b/api/apps' },
  { name: 'empty base', base: '', expected: '/api/apps' },
] as const;

describe('resolveApiUrl — mount points', () => {
  it.each(MOUNTS)('$name: base "$base" -> $expected', ({ base, expected }) => {
    expect(resolveApiUrl(base, APPS)).toBe(expected);
  });

  it('never hardcodes a prefix — an arbitrary user-chosen mount is honoured', () => {
    expect(resolveApiUrl('/some/deeply/nested/mount/', APPS)).toBe(
      '/some/deeply/nested/mount/api/apps',
    );
  });
});

describe('resolveApiUrl — path normalisation', () => {
  it.each([
    { path: '/api/apps', label: 'leading slash' },
    { path: 'api/apps', label: 'no leading slash' },
  ])('$label input resolves the same at the root mount', ({ path }) => {
    expect(resolveApiUrl('/', path)).toBe('/api/apps');
  });

  it.each([
    { path: '/api/apps', label: 'leading slash' },
    { path: 'api/apps', label: 'no leading slash' },
  ])('$label input resolves the same under a sub-path', ({ path }) => {
    expect(resolveApiUrl('/store/', path)).toBe('/store/api/apps');
  });

  it('keeps nested resource paths intact', () => {
    expect(resolveApiUrl('/store/', '/api/apps/jellyfin/install')).toBe(
      '/store/api/apps/jellyfin/install',
    );
  });

  it('preserves a query string verbatim', () => {
    const wizard = encodeURIComponent(JSON.stringify([{ key: 'port', value: '8096' }]));
    expect(resolveApiUrl('/store/', `/api/apps/jellyfin/install?wizard=${wizard}`)).toBe(
      `/store/api/apps/jellyfin/install?wizard=${wizard}`,
    );
  });

  it('does not touch encoded slashes inside a query string', () => {
    expect(resolveApiUrl('/store/', '/api/apps/x/diagnostic?error=a%2F%2Fb')).toBe(
      '/store/api/apps/x/diagnostic?error=a%2F%2Fb',
    );
  });
});

describe('resolveApiUrl — never emits a double slash', () => {
  const DIRTY_BASES = ['', '/', '//', '/store', '/store/', '//store//', 'store/', '/a/b/', '/a//b/'];
  const DIRTY_PATHS = ['/api/apps', 'api/apps', '//api/apps', '/api//apps'];

  it.each(
    DIRTY_BASES.flatMap((base) => DIRTY_PATHS.map((path) => ({ base, path }))),
  )('base "$base" + path "$path" stays single-slashed', ({ base, path }) => {
    const url = resolveApiUrl(base, path);
    expect(url).not.toContain('//');
    // A protocol-relative URL ("//host/...") would silently send credentials
    // to a third-party host, so assert the shape too, not just the absence.
    expect(url.startsWith('/')).toBe(true);
    expect(url.endsWith('/api/apps')).toBe(true);
  });
});
