// API client for the sciclaw addon system (Wave 4c).
//
// The backend endpoint `/api/addons/enabled` is provided by Wave 4b. While that
// wave is still in flight the endpoint may return 404 or a network error — the
// consumer code (see `src/addons/AddonTabs.tsx`) is expected to degrade
// gracefully to an empty tab list in that case, so this module throws on
// failure and leaves recovery to the caller.

// Addon represents a single enabled addon as reported by core sciclaw.
export interface Addon {
  name: string;
  version: string;
  state: 'installed' | 'enabled';
  ui_tab?: {
    name: string;
    icon?: string;
    path?: string;
  };
}

export async function fetchEnabledAddons(): Promise<Addon[]> {
  const res = await fetch('/api/addons/enabled');
  if (!res.ok) {
    throw new Error(`fetchEnabledAddons: ${res.status} ${res.statusText}`);
  }
  return res.json();
}
