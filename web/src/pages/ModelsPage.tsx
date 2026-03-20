import { useState, useEffect } from 'react';
import {
  Cpu, Check, RefreshCw, Loader2, Gauge, CheckCircle2, Circle, XCircle,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import EmptyState from '../components/EmptyState';
import {
  getModelInfo, getModelCatalog, setModel, setEffort,
  type ModelInfo, type ModelCatalogEntry,
} from '../lib/api';
import { serviceAction } from '../lib/api';
import { useSnapshot } from '../hooks/useSnapshot';

const effortLevels = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh'];
type ApplyStepStatus = 'pending' | 'running' | 'done' | 'error' | 'skipped';
type ApplyStep = { key: string; label: string; status: ApplyStepStatus; detail?: string };

export default function ModelsPage() {
  const { snapshot, refresh } = useSnapshot();
  const [info, setInfo] = useState<ModelInfo | null>(null);
  const [catalog, setCatalog] = useState<ModelCatalogEntry[]>([]);
  const [mode, setMode] = useState<'view' | 'select' | 'manual' | 'effort'>('view');
  const [manualInput, setManualInput] = useState('');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [selectedEffort, setSelectedEffort] = useState('');
  const [loading, setLoading] = useState(false);
  const [flash, setFlash] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [catalogSource, setCatalogSource] = useState('');
  const [catalogProvider, setCatalogProvider] = useState('');
  const [catalogWarning, setCatalogWarning] = useState('');
  const [catalogError, setCatalogError] = useState('');
  const [applyTitle, setApplyTitle] = useState('');
  const [applySteps, setApplySteps] = useState<ApplyStep[] | null>(null);
  const [applyOutput, setApplyOutput] = useState('');

  const updateStep = (key: string, patch: Partial<ApplyStep>) => {
    setApplySteps((steps) => steps?.map((step) => (step.key === key ? { ...step, ...patch } : step)) ?? null);
  };

  const fetchData = async () => {
    try {
      const [i, c] = await Promise.all([getModelInfo(), getModelCatalog()]);
      setInfo(i);
      setCatalog(c.models);
      setCatalogSource(c.source || '');
      setCatalogProvider(c.provider || '');
      setCatalogWarning(c.warning || '');
      setCatalogError('');
      setSelectedEffort(i.effort);
    } catch (e) {
      setCatalog([]);
      setCatalogProvider('');
      setCatalogSource('');
      setCatalogWarning('');
      setCatalogError(e instanceof Error ? e.message : 'Failed to load model catalog');
    } finally {
      setLoaded(true);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleSetModel = async (model: string) => {
    const trimmed = model.trim();
    if (!trimmed) return;
    setLoading(true);
    setApplyTitle(`Applying ${trimmed}`);
    setApplyOutput('');
    setApplySteps([
      { key: 'save', label: 'Save model and provider config', status: 'running' },
      { key: 'restart', label: snapshot.ServiceRunning ? 'Restart gateway service' : 'Gateway restart not needed', status: snapshot.ServiceRunning ? 'pending' : 'skipped' },
      { key: 'verify', label: 'Refresh live model status', status: 'pending' },
    ]);
    try {
      const result = await setModel(trimmed);
      updateStep('save', { status: 'done', detail: `${result.model} via ${result.provider}` });

      if (result.restartRequired) {
        updateStep('restart', { status: 'running' });
        const restart = await serviceAction('restart');
        setApplyOutput(restart.output || '');
        if (!restart.ok) {
          updateStep('restart', { status: 'error', detail: restart.output || 'Gateway restart failed' });
          throw new Error(restart.output || 'Gateway restart failed');
        }
        updateStep('restart', { status: 'done', detail: 'Gateway restarted' });
      }

      updateStep('verify', { status: 'running' });
      await Promise.all([fetchData(), refresh()]);
      updateStep('verify', { status: 'done', detail: 'Live status refreshed' });

      showFlash(result.restartRequired ? `Model set to ${trimmed} and gateway restarted` : `Model set to ${trimmed}`);
      await fetchData();
      setMode('view');
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setApplyOutput((current) => current || message);
      updateStep('verify', { status: 'error', detail: message });
      showFlash(`Error: ${message}`);
    } finally {
      setLoading(false);
    }
  };

  const handleSetEffort = async (effort: string) => {
    setLoading(true);
    setApplyTitle(`Applying reasoning effort ${effort}`);
    setApplyOutput('');
    setApplySteps([
      { key: 'save', label: 'Save reasoning effort config', status: 'running' },
      { key: 'restart', label: snapshot.ServiceRunning ? 'Restart gateway service' : 'Gateway restart not needed', status: snapshot.ServiceRunning ? 'pending' : 'skipped' },
      { key: 'verify', label: 'Refresh live model status', status: 'pending' },
    ]);
    try {
      await setEffort(effort);
      updateStep('save', { status: 'done', detail: effort });

      if (snapshot.ServiceRunning) {
        updateStep('restart', { status: 'running' });
        const restart = await serviceAction('restart');
        setApplyOutput(restart.output || '');
        if (!restart.ok) {
          updateStep('restart', { status: 'error', detail: restart.output || 'Gateway restart failed' });
          throw new Error(restart.output || 'Gateway restart failed');
        }
        updateStep('restart', { status: 'done', detail: 'Gateway restarted' });
      }

      updateStep('verify', { status: 'running' });
      await Promise.all([fetchData(), refresh()]);
      updateStep('verify', { status: 'done', detail: 'Live status refreshed' });

      showFlash(snapshot.ServiceRunning ? `Effort set to ${effort} and gateway restarted` : `Effort set to ${effort}`);
      setMode('view');
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setApplyOutput((current) => current || message);
      updateStep('verify', { status: 'error', detail: message });
      showFlash(`Error: ${message}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <TopBar title="Models" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}
        {applySteps && (
          <Card title={applyTitle}>
            <div className="space-y-3">
              <p className="text-xs text-zinc-500">
                Model changes only matter after the gateway is actually restarted. This flow shows each real step.
              </p>
              <div className="space-y-2">
                {applySteps.map((step) => {
                  const icon = step.status === 'running'
                    ? <Loader2 className="w-3.5 h-3.5 animate-spin text-brand" />
                    : step.status === 'done'
                      ? <CheckCircle2 className="w-3.5 h-3.5 text-brand" />
                      : step.status === 'error'
                        ? <XCircle className="w-3.5 h-3.5 text-red-400" />
                        : <Circle className={`w-3.5 h-3.5 ${step.status === 'skipped' ? 'text-zinc-700' : 'text-zinc-600'}`} />;
                  return (
                    <div key={step.key} className="flex items-start gap-3 rounded-md border border-border bg-surface-50/20 px-3 py-2">
                      <div className="mt-0.5">{icon}</div>
                      <div className="min-w-0">
                        <p className="text-sm text-zinc-200">{step.label}</p>
                        {step.detail && <p className="text-xs text-zinc-500">{step.detail}</p>}
                      </div>
                    </div>
                  );
                })}
              </div>
              {applyOutput && (
                <div className="rounded-md border border-border-subtle bg-surface-300 p-3">
                  <pre className="whitespace-pre-wrap text-xs leading-relaxed text-zinc-400 font-mono">{applyOutput}</pre>
                </div>
              )}
            </div>
          </Card>
        )}

        {/* Current model */}
        <Card
          title="Current Model"
          actions={
            <button onClick={() => { setLoaded(false); fetchData(); }} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          }
        >
          {!loaded && !info ? (
            <div className="h-20 bg-surface-50/30 rounded-md animate-pulse" />
          ) : info ? (
            <div className="space-y-3">
              {[
                ['Model', info.current || 'Not set'],
                ['Provider', info.provider || '—'],
                ['Auth', info.authMethod || '—'],
                ['Effort', info.effort || 'default'],
              ].map(([label, value]) => (
                <div key={label} className="flex items-center justify-between">
                  <span className="text-sm text-zinc-500">{label}</span>
                  <span className="text-sm text-zinc-300 font-mono">{value}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="rounded-md border border-border bg-surface-50/20 px-3 py-4 text-sm text-zinc-500">
              Could not load current model info.
            </div>
          )}
        </Card>

        {/* Actions */}
        <div className="flex gap-2">
          <button
            onClick={() => setMode(mode === 'select' ? 'view' : 'select')}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md font-medium transition-colors ${
              mode === 'select' ? 'bg-brand text-surface-500' : 'border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50'
            }`}
          >
            <Cpu className="w-3 h-3" />
            Select from Catalog
          </button>
          <button
            onClick={() => setMode(mode === 'manual' ? 'view' : 'manual')}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md font-medium transition-colors ${
              mode === 'manual' ? 'bg-brand text-surface-500' : 'border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50'
            }`}
          >
            Manual Entry
          </button>
          <button
            onClick={() => { setMode(mode === 'effort' ? 'view' : 'effort'); }}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md font-medium transition-colors ${
              mode === 'effort' ? 'bg-brand text-surface-500' : 'border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50'
            }`}
          >
            <Gauge className="w-3 h-3" />
            Reasoning Effort
          </button>
        </div>

        {/* Model catalog */}
        {mode === 'select' && (
          <Card title="Model Catalog">
            <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
              {catalogProvider && (
                <StatusBadge variant="info">{catalogProvider}</StatusBadge>
              )}
              {catalogSource && (
                <span className="rounded-md border border-border px-2 py-1 text-zinc-500">
                  Source: <span className="font-mono text-zinc-300">{catalogSource}</span>
                </span>
              )}
              {catalogWarning && (
                <span className="rounded-md border border-amber-500/20 bg-amber-500/10 px-2 py-1 text-amber-300">
                  {catalogWarning}
                </span>
              )}
            </div>
            {!loaded ? (
              <div className="flex items-center justify-center py-8 gap-2 text-sm text-zinc-500">
                <Loader2 className="w-4 h-4 animate-spin" />
                Loading catalog...
              </div>
            ) : catalogError ? (
              <div className="rounded-md border border-red-500/20 bg-red-500/10 px-3 py-4 text-sm text-red-300">
                {catalogError}
              </div>
            ) : catalog.length === 0 ? (
              <EmptyState
                icon={Cpu}
                title="No catalog entries"
                description="No models were discovered for the current provider configuration."
              />
            ) : (
              <div className="divide-y divide-border-subtle max-h-96 overflow-y-auto">
                {catalog.map((m, idx) => (
                  <button
                    key={m.id}
                    onClick={() => setSelectedIdx(idx)}
                    onDoubleClick={() => handleSetModel(m.id)}
                    className={`flex items-center justify-between w-full text-left px-3 py-2.5 transition-colors ${
                      selectedIdx === idx ? 'bg-brand/5' : 'hover:bg-surface-50/30'
                    }`}
                  >
                    <div className="flex items-center gap-3">
                      <Cpu className={`w-4 h-4 ${selectedIdx === idx ? 'text-brand' : 'text-zinc-600'}`} />
                      <div>
                        <p className="text-sm text-zinc-200 font-mono">{m.name || m.id}</p>
                        <p className="text-xs text-zinc-600">{m.provider} · {m.source}</p>
                      </div>
                    </div>
                    {info?.current === m.id && (
                      <StatusBadge variant="ready">Current</StatusBadge>
                    )}
                  </button>
                ))}
              </div>
            )}
            {catalog.length > 0 && (
              <div className="mt-3 pt-3 border-t border-border-subtle flex gap-2">
                <button
                  onClick={() => handleSetModel(catalog[selectedIdx].id)}
                  disabled={loading}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                >
                  {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <Check className="w-3 h-3" />}
                  Use Selected
                </button>
              </div>
            )}
          </Card>
        )}

        {/* Manual entry */}
        {mode === 'manual' && (
          <Card title="Manual Model Entry">
            <div className="space-y-3">
              <p className="text-xs text-zinc-500">
                Enter a model identifier, e.g. <code className="text-zinc-400 bg-surface-50 px-1 rounded font-mono">gpt-5.4</code> or{' '}
                <code className="text-zinc-400 bg-surface-50 px-1 rounded font-mono">claude-sonnet-4.6</code>
              </p>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={manualInput}
                  onChange={(e) => setManualInput(e.target.value)}
                  placeholder="Model identifier..."
                  className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 font-mono placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                  autoFocus
                  onKeyDown={(e) => e.key === 'Enter' && handleSetModel(manualInput.trim())}
                />
                <button
                  onClick={() => handleSetModel(manualInput.trim())}
                  disabled={!manualInput.trim() || loading}
                  className="flex items-center gap-1.5 px-4 py-2 text-sm rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                >
                  {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}
                  Set
                </button>
              </div>
            </div>
          </Card>
        )}

        {/* Effort selector */}
        {mode === 'effort' && (
          <Card title="Reasoning Effort">
            <div className="space-y-3">
              <p className="text-xs text-zinc-500">
                Adjust how much reasoning the model uses for each response.
              </p>
              <div className="flex gap-1 p-1 rounded-lg bg-surface-50/30 border border-border">
                {effortLevels.map((level) => (
                  <button
                    key={level}
                    onClick={() => setSelectedEffort(level)}
                    className={`flex-1 px-3 py-2 text-xs rounded-md font-medium transition-colors ${
                      selectedEffort === level
                        ? 'bg-brand text-surface-500'
                        : 'text-zinc-400 hover:text-zinc-200 hover:bg-surface-50'
                    }`}
                  >
                    {level}
                  </button>
                ))}
              </div>
              <button
                onClick={() => handleSetEffort(selectedEffort)}
                disabled={loading || selectedEffort === info?.effort}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
              >
                {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <Check className="w-3 h-3" />}
                Apply
              </button>
            </div>
          </Card>
        )}
      </main>
    </>
  );
}
