import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { getSnapshot, type Snapshot } from '../lib/api';

const defaultSnapshot: Snapshot = {
  State: 'unknown',
  IPv4: '',
  Load: '',
  Memory: '',
  ConfigExists: false,
  WorkspacePath: '',
  AuthStoreExists: false,
  OpenAI: 'missing',
  Anthropic: 'missing',
  ActiveModel: '',
  ActiveProvider: '',
  Discord: { Status: 'off', Enabled: false, HasToken: false, ApprovedUsers: [] },
  Telegram: { Status: 'off', Enabled: false, HasToken: false, ApprovedUsers: [] },
  ServiceInstalled: false,
  ServiceRunning: false,
  ServiceAutoStart: false,
};

export const SnapshotContext = createContext<{
  snapshot: Snapshot;
  loading: boolean;
  refresh: () => void;
}>({ snapshot: defaultSnapshot, loading: true, refresh: () => {} });

export function useSnapshotProvider() {
  const [snapshot, setSnapshot] = useState<Snapshot>(defaultSnapshot);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const data = await getSnapshot();
      setSnapshot(prev => {
        // Avoid unnecessary re-renders when data hasn't changed
        if (JSON.stringify(prev) === JSON.stringify(data)) return prev;
        return data;
      });
    } catch {
      // API not ready yet - keep defaults
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 10_000);
    return () => clearInterval(interval);
  }, [refresh]);

  return { snapshot, loading, refresh };
}

export const useSnapshot = () => useContext(SnapshotContext);
