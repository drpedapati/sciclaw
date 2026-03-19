import { useState, useEffect } from 'react';
import {
  Brain, RefreshCw, Play, Square, Download, FlaskConical,
  Loader2, CheckCircle2, XCircle, Pencil, Check,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import { getPhiData, phiAction, type PhiData } from '../lib/api';

export default function PhiPage() {
  const [data, setData] = useState<PhiData | null>(null);
  const [actionRunning, setActionRunning] = useState<string | null>(null);
  const [editModel, setEditModel] = useState(false);
  const [modelInput, setModelInput] = useState('');
  const [flash, setFlash] = useState('');

  const fetchData = async () => {
    try {
      const d = await getPhiData();
      setData(d);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleAction = async (action: string, params?: Record<string, string>) => {
    setActionRunning(action);
    try {
      const result = await phiAction(action, params);
      showFlash(result.ok ? `${action} completed` : result.output);
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setActionRunning(null);
    }
  };

  const ActionBtn = ({
    icon: Icon,
    label,
    action,
    variant = 'default',
    params,
  }: {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    action: string;
    variant?: 'default' | 'primary' | 'danger';
    params?: Record<string, string>;
  }) => {
    const running = actionRunning === action;
    const s = {
      default: 'border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50',
      primary: 'bg-brand text-surface-500 hover:bg-brand-500',
      danger: 'border border-red-500/30 text-red-400 hover:bg-red-500/10',
    };
    return (
      <button
        onClick={() => handleAction(action, params)}
        disabled={!!actionRunning}
        className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md disabled:opacity-50 transition-colors font-medium ${s[variant]}`}
      >
        {running ? <Loader2 className="w-3 h-3 animate-spin" /> : <Icon className="w-3 h-3" />}
        {label}
      </button>
    );
  };

  return (
    <>
      <TopBar title="PHI (Local Runtime)" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          {/* Configuration */}
          <Card
            title="Configuration"
            actions={
              <button onClick={fetchData} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
                <RefreshCw className="w-3.5 h-3.5" />
              </button>
            }
          >
            {!data ? (
              <div className="space-y-3">
                {[1, 2, 3, 4, 5].map((i) => (
                  <div key={i} className="h-8 rounded bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : (
              <div className="space-y-3 text-sm">
                {[
                  ['Mode', data.mode],
                  ['Cloud Model', data.cloudModel || '—'],
                  ['Cloud Provider', data.cloudProvider || '—'],
                  ['Local Backend', data.localBackend || '—'],
                  ['Local Preset', data.localPreset || '—'],
                ].map(([label, value]) => (
                  <div key={label} className="flex items-center justify-between">
                    <span className="text-zinc-500">{label}</span>
                    <span className="text-zinc-300 font-mono text-xs">{value}</span>
                  </div>
                ))}
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Local Model</span>
                  {editModel ? (
                    <div className="flex items-center gap-1.5">
                      <input
                        type="text"
                        value={modelInput}
                        onChange={(e) => setModelInput(e.target.value)}
                        className="w-40 rounded border border-border bg-surface-100 px-2 py-1 text-xs text-zinc-200 font-mono focus:outline-none focus:ring-1 focus:ring-brand/50"
                        autoFocus
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') { handleAction('set-model', { model: modelInput }); setEditModel(false); }
                          if (e.key === 'Escape') setEditModel(false);
                        }}
                      />
                      <button onClick={() => { handleAction('set-model', { model: modelInput }); setEditModel(false); }} className="text-brand">
                        <Check className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => { setEditModel(true); setModelInput(data.localModel || ''); }}
                      className="flex items-center gap-1.5 text-zinc-300 font-mono text-xs hover:text-zinc-100 transition-colors"
                    >
                      {data.localModel || '—'}
                      <Pencil className="w-3 h-3 text-zinc-600" />
                    </button>
                  )}
                </div>
              </div>
            )}
          </Card>

          {/* Status */}
          <Card title="Status">
            {!data ? (
              <div className="space-y-3">
                {[1, 2, 3, 4].map((i) => (
                  <div key={i} className="h-8 rounded bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : (
              <div className="space-y-3 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Backend</span>
                  <StatusBadge variant={data.backendRunning ? 'ready' : 'muted'} dot>
                    {data.backendRunning ? 'Running' : 'Stopped'}
                  </StatusBadge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Installed</span>
                  {data.backendInstalled ? (
                    <span className="flex items-center gap-1 text-brand"><CheckCircle2 className="w-3.5 h-3.5" /> Yes</span>
                  ) : (
                    <span className="flex items-center gap-1 text-zinc-500"><XCircle className="w-3.5 h-3.5" /> No</span>
                  )}
                </div>
                {data.backendVersion && (
                  <div className="flex items-center justify-between">
                    <span className="text-zinc-500">Version</span>
                    <span className="text-zinc-300 font-mono text-xs">{data.backendVersion}</span>
                  </div>
                )}
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Model Ready</span>
                  {data.modelReady ? (
                    <span className="flex items-center gap-1 text-brand"><CheckCircle2 className="w-3.5 h-3.5" /> Yes</span>
                  ) : (
                    <span className="flex items-center gap-1 text-zinc-500"><XCircle className="w-3.5 h-3.5" /> No</span>
                  )}
                </div>
                {data.hardware && (
                  <div className="flex items-center justify-between">
                    <span className="text-zinc-500">Hardware</span>
                    <span className="text-zinc-300 font-mono text-xs">{data.hardware}</span>
                  </div>
                )}
              </div>
            )}
          </Card>
        </div>

        {/* Evaluation */}
        <Card title="Evaluation">
          {!data ? (
            <div className="h-16 rounded bg-surface-50/30 animate-pulse" />
          ) : (
            <div className="space-y-3">
              <div className="space-y-2 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500">Last Eval</span>
                  <span className="text-zinc-300 font-mono text-xs">{data.lastEval || 'Never'}</span>
                </div>
                {data.probeStatus && (
                  <div className="flex items-center justify-between">
                    <span className="text-zinc-500">Probes</span>
                    <span className="text-zinc-300 text-xs">{data.probeStatus}</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </Card>

        {/* Actions */}
        <div className="flex flex-wrap gap-2">
          <ActionBtn icon={Download} label="Setup Backend" action="setup" variant="primary" />
          <ActionBtn icon={Download} label="Install Model" action="install" />
          <ActionBtn icon={Play} label="Start" action="start" />
          <ActionBtn icon={Square} label="Stop" action="stop" variant="danger" />
          <ActionBtn icon={FlaskConical} label="Run Eval" action="eval" />
        </div>
      </main>
    </>
  );
}
