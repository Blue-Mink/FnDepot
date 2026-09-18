import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));

/** Repo root of fnos-store (frontend/tests/e2e -> ../../..). */
export const repoRoot = path.resolve(here, '..', '..', '..');

/** Git-tracked fixture the dev backend normally reads. Never mutated by e2e. */
export const sourceMockApps = path.join(repoRoot, 'dev', 'mock-apps');

/**
 * Disposable copy of dev/mock-apps that the e2e backend points at through
 * APPS_DIR + MOCK_APPS_DIR. Uninstall is destructive by definition, so these
 * tests must not run against the git-tracked fixture.
 */
export const e2eMockApps = path.join(os.tmpdir(), 'fnos-store-e2e', 'mock-apps');

/** Hardcoded state dir of dev/mock-appcenter-cli.sh. */
export const mockStateDir = '/tmp/fnos-store-dev/state';

/** Restore the disposable fixture to a pristine copy of dev/mock-apps. */
export function resetMockApps(): void {
  fs.rmSync(e2eMockApps, { recursive: true, force: true });
  fs.mkdirSync(path.dirname(e2eMockApps), { recursive: true });
  fs.cpSync(sourceMockApps, e2eMockApps, { recursive: true });
  fs.rmSync(mockStateDir, { recursive: true, force: true });
}

/** Apps that dev/mock-apps really ships. Selectors below must match these. */
export const FIXTURE = {
  /** installed, uninstalled by the card test */
  cardApp: { appname: 'jellyfin', displayName: 'Jellyfin' },
  /** installed, uninstalled by the detail-dialog test */
  dialogApp: { appname: 'qBittorrent', displayName: 'qBittorrent' },
  /** installed, kept installed — used for in-flight + regression checks */
  keepApp: { appname: 'plexmediaserver', displayName: 'Plex' },
  /** not installed in the fixture */
  notInstalledApp: { appname: 'transmission', displayName: 'Transmission' },
} as const;
