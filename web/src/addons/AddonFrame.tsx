// AddonFrame hosts an addon's UI inside an iframe sourced from
// `/addons/<name>/ui/`. Per the RFC, addon UIs are reverse-proxied from their
// sidecar process so they can ship their own framework (React, Vue, plain
// HTML) without having to match sciclaw's React version. The iframe boundary
// is the stable contract.

interface AddonFrameProps {
  addonName: string;
}

export function AddonFrame({ addonName }: AddonFrameProps) {
  return (
    <iframe
      src={`/addons/${addonName}/ui/`}
      className="w-full h-full border-0"
      title={`${addonName} addon UI`}
      sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
      allow="clipboard-read; clipboard-write; fullscreen"
    />
  );
}
