import { useState, useEffect } from 'react';
import { LogIn, LogOut, Key, Loader2, Shield } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import { getAuthStatus, loginProvider, logoutProvider, setApiKey, type AuthStatus } from '../lib/api';

export default function LoginPage() {
  const [providers, setProviders] = useState<AuthStatus[]>([]);
  const [selected, setSelected] = useState(0);
  const [keyMode, setKeyMode] = useState<string | null>(null);
  const [keyValue, setKeyValue] = useState('');
  const [loading, setLoading] = useState<string | null>(null);
  const [flash, setFlash] = useState('');

  const fetchData = async () => {
    try {
      const data = await getAuthStatus();
      setProviders(data);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleLogin = async (provider: string) => {
    setLoading(provider);
    try {
      const result = await loginProvider(provider);
      showFlash(result.ok ? `${provider} login initiated` : result.output);
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setLoading(null);
    }
  };

  const handleLogout = async (provider: string) => {
    setLoading(provider);
    try {
      await logoutProvider(provider);
      showFlash(`${provider} logged out`);
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setLoading(null);
    }
  };

  const handleSetKey = async (provider: string) => {
    if (!keyValue.trim()) return;
    setLoading(provider);
    try {
      await setApiKey(provider, keyValue.trim());
      showFlash(`${provider} API key set`);
      setKeyMode(null);
      setKeyValue('');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setLoading(null);
    }
  };

  return (
    <>
      <TopBar title="Login" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <Card title="AI Providers">
          {providers.length === 0 ? (
            <div className="space-y-3">
              {[1, 2].map((i) => (
                <div key={i} className="h-16 rounded-md bg-surface-50/30 animate-pulse" />
              ))}
            </div>
          ) : (
            <div className="space-y-3">
              {providers.map((p, idx) => (
                <div
                  key={p.provider}
                  onClick={() => setSelected(idx)}
                  className={`rounded-lg border p-4 transition-colors duration-150 cursor-pointer ${
                    selected === idx
                      ? 'border-brand/30 bg-brand/5'
                      : 'border-border hover:bg-surface-50/30'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <Shield className="w-5 h-5 text-zinc-500" />
                      <div>
                        <p className="text-sm font-medium text-zinc-200">{p.provider}</p>
                        {p.method && (
                          <p className="text-xs text-zinc-500 mt-0.5">via {p.method}</p>
                        )}
                      </div>
                    </div>
                    <StatusBadge
                      variant={p.status === 'active' ? 'ready' : 'muted'}
                      dot
                    >
                      {p.status === 'active' ? 'Active' : 'Not Set'}
                    </StatusBadge>
                  </div>

                  {/* Actions for selected provider */}
                  {selected === idx && (
                    <div className="mt-4 pt-3 border-t border-border-subtle">
                      {keyMode === p.provider ? (
                        <div className="flex gap-2">
                          <input
                            type="password"
                            value={keyValue}
                            onChange={(e) => setKeyValue(e.target.value)}
                            placeholder="Paste API key..."
                            className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                            autoFocus
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') handleSetKey(p.provider);
                              if (e.key === 'Escape') { setKeyMode(null); setKeyValue(''); }
                            }}
                          />
                          <button
                            onClick={() => handleSetKey(p.provider)}
                            disabled={!keyValue.trim()}
                            className="px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                          >
                            Save
                          </button>
                          <button
                            onClick={() => { setKeyMode(null); setKeyValue(''); }}
                            className="px-3 py-1.5 text-xs rounded-md border border-border text-zinc-400 hover:bg-surface-50 transition-colors"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <div className="flex gap-2">
                          <button
                            onClick={() => handleLogin(p.provider)}
                            disabled={loading === p.provider}
                            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                          >
                            {loading === p.provider ? <Loader2 className="w-3 h-3 animate-spin" /> : <LogIn className="w-3 h-3" />}
                            Login
                          </button>
                          <button
                            onClick={() => setKeyMode(p.provider)}
                            className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50 transition-colors"
                          >
                            <Key className="w-3 h-3" />
                            Set API Key
                          </button>
                          {p.status === 'active' && (
                            <button
                              onClick={() => handleLogout(p.provider)}
                              disabled={loading === p.provider}
                              className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md border border-red-500/30 text-red-400 hover:bg-red-500/10 transition-colors"
                            >
                              <LogOut className="w-3 h-3" />
                              Logout
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </Card>
      </main>
    </>
  );
}
