import { FolderOpen, ChevronRight, Loader2 } from 'lucide-react';
import type { BrowseResponse } from '../lib/api';

export default function DirectoryBrowser({
  currentPath,
  dirs,
  loading,
  error,
  onNavigate,
  onSelect,
}: {
  currentPath: string;
  dirs: BrowseResponse['dirs'];
  loading: boolean;
  error: string;
  onNavigate: (path: string) => void;
  onSelect: (path: string) => void;
}) {
  // Build breadcrumb segments from an absolute path.
  const segments = currentPath.split('/').filter(Boolean);
  const crumbs = segments.map((seg, i) => ({
    label: seg,
    path: '/' + segments.slice(0, i + 1).join('/'),
  }));

  const parentPath = segments.length > 0
    ? ('/' + segments.slice(0, -1).join('/')) || '/'
    : null;

  return (
    <div className="rounded-md border border-border bg-surface-100 overflow-hidden">
      {/* Breadcrumb bar */}
      <div className="flex items-center gap-0.5 px-3 py-2 border-b border-border-subtle flex-wrap">
        <button
          onClick={() => onNavigate('/')}
          className="text-xs text-zinc-400 hover:text-zinc-200 transition-colors"
        >
          /
        </button>
        {crumbs.map((crumb, i) => (
          <span key={crumb.path} className="flex items-center gap-0.5">
            <ChevronRight className="w-3 h-3 text-zinc-600" />
            {i === crumbs.length - 1 ? (
              <span className="text-xs text-zinc-200 font-medium">{crumb.label}</span>
            ) : (
              <button
                onClick={() => onNavigate(crumb.path)}
                className="text-xs text-zinc-400 hover:text-zinc-200 transition-colors"
              >
                {crumb.label}
              </button>
            )}
          </span>
        ))}
      </div>

      {/* Directory list */}
      <div className="max-h-56 overflow-y-auto">
        {loading && (
          <div className="flex items-center justify-center py-6 gap-2 text-zinc-500 text-xs">
            <Loader2 className="w-4 h-4 animate-spin" />
            Loading...
          </div>
        )}
        {!loading && error && (
          <div className="px-3 py-3 text-xs text-red-400">{error}</div>
        )}
        {!loading && !error && (
          <div className="divide-y divide-border-subtle">
            {parentPath !== null && (
              <button
                onClick={() => onNavigate(parentPath)}
                className="flex items-center gap-2 w-full text-left px-3 py-2 text-xs text-zinc-400 hover:bg-surface-50 transition-colors"
              >
                <FolderOpen className="w-3.5 h-3.5 text-zinc-600" />
                ..
              </button>
            )}
            {dirs.length === 0 && (
              <p className="px-3 py-3 text-xs text-zinc-500">No subdirectories</p>
            )}
            {dirs.map((d) => {
              const childPath = currentPath.replace(/\/$/, '') + '/' + d.name;
              return (
                <button
                  key={d.name}
                  onClick={() => onNavigate(childPath)}
                  className="flex items-center gap-2 w-full text-left px-3 py-2 text-xs text-zinc-300 hover:bg-surface-50 transition-colors"
                >
                  <FolderOpen className="w-3.5 h-3.5 text-zinc-500" />
                  {d.name}
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Select button */}
      {!loading && !error && (
        <div className="px-3 py-2 border-t border-border-subtle">
          <button
            onClick={() => onSelect(currentPath)}
            className="w-full px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
          >
            Select this folder
          </button>
        </div>
      )}
    </div>
  );
}
