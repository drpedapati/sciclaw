// AddonPage wraps an addon's iframe UI in the standard TopBar layout so the
// dynamic `/addons/:name` routes match the chrome used by the built-in pages.
// The iframe itself is in `src/addons/AddonFrame.tsx`.

import { useParams } from 'react-router-dom';
import TopBar from '../components/TopBar';
import { AddonFrame } from '../addons/AddonFrame';
import { useEnabledAddons } from '../addons/AddonTabs';

export default function AddonPage() {
  const { name = '' } = useParams<{ name: string }>();
  const addons = useEnabledAddons();
  const addon = addons.find((a) => a.name === name);
  const title = addon?.ui_tab?.name ?? name;

  return (
    <>
      <TopBar title={title} />
      <main className="flex-1 overflow-hidden">
        <AddonFrame addonName={name} />
      </main>
    </>
  );
}
