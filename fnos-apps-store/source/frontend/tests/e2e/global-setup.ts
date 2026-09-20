import { resetMockApps } from './fixture';

/**
 * Runs before Playwright boots the webServers, so the Go backend starts against
 * a pristine, disposable copy of dev/mock-apps.
 */
export default function globalSetup(): void {
  resetMockApps();
}
