// AddonTabs renders dynamic navigation entries for every addon that has a
// ui_tab declared in its manifest. The list is fetched from the backend
// endpoint `/api/addons/enabled` (Wave 4b) and refreshed every 30 seconds so
// newly enabled or disabled addons appear without a page reload.
//
// Graceful degradation: while the backend endpoint is missing (404) or a
// network error occurs, the hook returns an empty array and logs a single
// debug line — it never throws, so the Sidebar continues to render normally.
//
// The <AddonTabs> component honours the contract defined in the Wave 4c task
// spec (`activeTab` + `onSelectTab` props) so it can be dropped into any tab
// system. The Sidebar integration instead uses the `useEnabledAddons` hook
// directly and renders NavLinks, matching the existing react-router pattern
// used throughout the app.

import { useEffect, useState } from 'react';
import { Puzzle } from 'lucide-react';
import { fetchEnabledAddons, type Addon } from '../api/addons';

// Poll interval for `/api/addons/enabled`. Matches the 30s cadence from the
// task spec — long enough to avoid hammering the backend, short enough that
// enabling an addon feels ~live.
const POLL_INTERVAL_MS = 30_000;

// `loggedMissingOnce` keeps the dev console quiet when Wave 4b hasn't shipped.
// The first fetch failure logs a single info line; subsequent failures are
// silent until the next successful fetch resets the flag.
let loggedMissingOnce = false;

export function useEnabledAddons(): Addon[] {
  const [addons, setAddons] = useState<Addon[]>([]);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const list = await fetchEnabledAddons();
        if (cancelled) return;
        setAddons(list);
        loggedMissingOnce = false;
      } catch (err) {
        if (cancelled) return;
        if (!loggedMissingOnce) {
          // eslint-disable-next-line no-console
          console.info('[addons] /api/addons/enabled unavailable, tab list empty:', err);
          loggedMissingOnce = true;
        }
        setAddons([]);
      }
    };

    load();
    const id = window.setInterval(load, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, []);

  return addons;
}

interface AddonTabsProps {
  activeTab?: string;
  onSelectTab: (name: string) => void;
}

// Standalone tab-list rendering used by consumers that manage their own tab
// state (see task spec Wave 4c). The Sidebar integration uses NavLinks via
// `useEnabledAddons` directly instead of this component.
export function AddonTabs({ activeTab, onSelectTab }: AddonTabsProps) {
  const addons = useEnabledAddons();
  const withTab = addons.filter((a) => a.ui_tab);

  if (withTab.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-1" role="tablist" aria-label="Addons">
      {withTab.map((addon) => {
        const tabName = addon.ui_tab!.name;
        const tabId = `addon:${addon.name}`;
        const isActive = activeTab === tabId;
        return (
          <button
            key={addon.name}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onSelectTab(tabId)}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm transition-colors duration-150 ${
              isActive
                ? 'bg-brand/10 text-brand border border-brand/30'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-surface-50/50'
            }`}
          >
            <Puzzle className="w-4 h-4 flex-shrink-0" />
            {tabName}
          </button>
        );
      })}
    </div>
  );
}
