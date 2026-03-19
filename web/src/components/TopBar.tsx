import { RefreshCw } from 'lucide-react';
import { useSnapshot } from '../hooks/useSnapshot';

export default function TopBar({ title }: { title: string }) {
  const { snapshot, refresh } = useSnapshot();

  return (
    <header className="h-14 flex items-center justify-between px-6 border-b border-border bg-surface-200 flex-shrink-0">
      <h1 className="text-lg font-semibold text-zinc-100">{title}</h1>
      <div className="flex items-center gap-4">
        {snapshot.ActiveProvider && (
          <span className="text-xs text-zinc-500">
            {snapshot.ActiveProvider}
          </span>
        )}
        <button
          onClick={refresh}
          className="p-1.5 rounded-md hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors duration-150"
          title="Refresh"
        >
          <RefreshCw className="w-4 h-4" />
        </button>
      </div>
    </header>
  );
}
