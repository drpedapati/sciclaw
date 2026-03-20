import { useState, useEffect } from 'react';
import {
  GitBranch, Plus, Trash2, RefreshCw, Loader2, Radio,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import {
  getRoutingStatus, getRoutingMappings, addRoutingMapping,
  removeRoutingMapping, routingReload,
  type RoutingStatus, type RoutingMapping,
} from '../lib/api';

export default function RoutingPage() {
  const [status, setStatus] = useState<RoutingStatus | null>(null);
  const [mappings, setMappings] = useState<RoutingMapping[]>([]);
  const [selected, setSelected] = useState(0);
  const [addMode, setAddMode] = useState(false);
  const [addStep, setAddStep] = useState(0);
  const [addData, setAddData] = useState({ channel: 'discord', chatId: '', workspace: '', label: '' });
  const [deleteMapping, setDeleteMapping] = useState<RoutingMapping | null>(null);
  const [flash, setFlash] = useState('');
  const [reloading, setReloading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState('');

  const fetchData = async () => {
    try {
      const [s, m] = await Promise.all([getRoutingStatus(), getRoutingMappings()]);
      setStatus(s);
      setMappings(m);
      setLoadError('');
      setSelected((idx) => (m.length === 0 ? 0 : Math.min(idx, m.length - 1)));
    } catch (e) {
      setStatus(null);
      setMappings([]);
      setLoadError(e instanceof Error ? e.message : 'Failed to load routing state');
    } finally {
      setLoaded(true);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleAdd = async () => {
    if (addStep < 3) { setAddStep((s) => s + 1); return; }
    try {
      await addRoutingMapping(addData);
      showFlash('Mapping added');
      setAddMode(false);
      setAddStep(0);
      setAddData({ channel: 'discord', chatId: '', workspace: '', label: '' });
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
  };

  const handleRemove = async () => {
    if (!deleteMapping) return;
    try {
      await removeRoutingMapping(deleteMapping.id);
      showFlash('Mapping removed');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setDeleteMapping(null);
  };

  const handleReload = async () => {
    setReloading(true);
    try {
      await routingReload();
      showFlash('Routing reloaded');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setReloading(false);
    }
  };

  const selectedMapping = mappings[selected];

  return (
    <>
      <TopBar title="Routing" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}
        {loadError && (
          <div className="rounded-md border border-red-500/20 bg-red-500/10 px-4 py-2 text-sm text-red-300 animate-fade-in">
            {loadError}
          </div>
        )}

        {/* Status overview */}
        {status && (
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <div className="rounded-lg border border-border bg-surface-100 p-4">
              <p className="text-xs text-zinc-500 mb-1">Routing</p>
              <StatusBadge variant={status.enabled ? 'ready' : 'muted'} dot>
                {status.enabled ? 'Enabled' : 'Disabled'}
              </StatusBadge>
            </div>
            <div className="rounded-lg border border-border bg-surface-100 p-4">
              <p className="text-xs text-zinc-500 mb-1">Unmapped Behavior</p>
              <span className="text-sm text-zinc-300 font-mono">{status.unmappedBehavior}</span>
            </div>
            <div className="rounded-lg border border-border bg-surface-100 p-4">
              <p className="text-xs text-zinc-500 mb-1">Total Mappings</p>
              <span className="text-lg font-semibold text-zinc-200">{status.totalMappings}</span>
            </div>
            <div className="rounded-lg border border-border bg-surface-100 p-4">
              <p className="text-xs text-zinc-500 mb-1">Invalid</p>
              <span className={`text-lg font-semibold ${status.invalidMappings > 0 ? 'text-red-400' : 'text-zinc-200'}`}>
                {status.invalidMappings}
              </span>
            </div>
          </div>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
          {/* Mappings list */}
          <div className="lg:col-span-2">
            <Card
              title="Route Mappings"
              actions={
                <div className="flex gap-2">
                  <button
                    onClick={handleReload}
                    disabled={reloading}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50 transition-colors"
                  >
                    {reloading ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
                    Reload
                  </button>
                  <button
                    onClick={() => setAddMode(true)}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
                  >
                    <Plus className="w-3 h-3" />
                    Add Mapping
                  </button>
                </div>
              }
            >
              {/* Add wizard */}
              {addMode && (
                <div className="mb-4 p-3 rounded-md border border-brand/20 bg-brand/5 space-y-3 animate-fade-in">
                  {addStep === 0 && (
                    <div>
                      <label className="text-xs text-zinc-500 block mb-2">Channel</label>
                      <div className="flex gap-2">
                        {(['discord', 'telegram'] as const).map((ch) => (
                          <button
                            key={ch}
                            onClick={() => { setAddData({ ...addData, channel: ch }); setAddStep(1); }}
                            className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm border transition-colors ${
                              addData.channel === ch ? 'border-brand/30 bg-brand/10 text-brand' : 'border-border text-zinc-400 hover:bg-surface-50'
                            }`}
                          >
                            <Radio className="w-3.5 h-3.5" />
                            {ch === 'discord' ? 'Discord' : 'Telegram'}
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                  {addStep === 1 && (
                    <div>
                      <label className="text-xs text-zinc-500 block mb-1">Chat ID</label>
                      <input
                        type="text"
                        value={addData.chatId}
                        onChange={(e) => setAddData({ ...addData, chatId: e.target.value })}
                        placeholder="Channel/chat ID"
                        className="w-full rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 font-mono placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                        autoFocus
                      />
                    </div>
                  )}
                  {addStep === 2 && (
                    <div>
                      <label className="text-xs text-zinc-500 block mb-1">Workspace Path</label>
                      <input
                        type="text"
                        value={addData.workspace}
                        onChange={(e) => setAddData({ ...addData, workspace: e.target.value })}
                        placeholder="/path/to/workspace"
                        className="w-full rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 font-mono placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                        autoFocus
                      />
                    </div>
                  )}
                  {addStep === 3 && (
                    <div>
                      <label className="text-xs text-zinc-500 block mb-1">Label (optional)</label>
                      <input
                        type="text"
                        value={addData.label}
                        onChange={(e) => setAddData({ ...addData, label: e.target.value })}
                        placeholder="e.g. Research Lab"
                        className="w-full rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                        autoFocus
                      />
                    </div>
                  )}
                  <div className="flex gap-2 pt-1">
                    <button onClick={() => { setAddMode(false); setAddStep(0); }} className="px-3 py-1.5 text-xs rounded-md border border-border text-zinc-400 hover:bg-surface-50 transition-colors">
                      Cancel
                    </button>
                    <button onClick={handleAdd} className="px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium">
                      {addStep < 3 ? 'Next' : 'Add'}
                    </button>
                  </div>
                </div>
              )}

              {!loaded ? (
                <div className="space-y-2">
                  {[1, 2, 3].map((i) => (
                    <div key={i} className="h-12 rounded-md bg-surface-50/30 animate-pulse" />
                  ))}
                </div>
              ) : mappings.length === 0 ? (
                <EmptyState
                  icon={GitBranch}
                  title="No route mappings"
                  description="Map Discord/Telegram channels to workspaces."
                />
              ) : (
                <div className="divide-y divide-border-subtle">
                  {mappings.map((m, idx) => (
                    <button
                      key={m.id}
                      onClick={() => setSelected(idx)}
                      className={`flex items-center justify-between w-full text-left px-3 py-3 transition-colors group ${
                        selected === idx ? 'bg-brand/5' : 'hover:bg-surface-50/30'
                      }`}
                    >
                      <div className="flex items-center gap-3 min-w-0">
                        <StatusBadge variant={m.channel === 'discord' ? 'info' : 'ready'}>
                          {m.channel}
                        </StatusBadge>
                        <div className="min-w-0">
                          <p className="text-sm text-zinc-200 truncate">{m.label || m.chatId}</p>
                          <p className="text-xs text-zinc-600 font-mono truncate">{m.workspace}</p>
                        </div>
                      </div>
                      <button
                        onClick={(e) => { e.stopPropagation(); setDeleteMapping(m); }}
                        className="p-1 rounded hover:bg-red-500/10 text-zinc-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-all"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </button>
                  ))}
                </div>
              )}
            </Card>
          </div>

          {/* Detail pane */}
          <div>
            <Card title="Mapping Details">
              {selectedMapping ? (
                <div className="space-y-3 text-sm">
                  {[
                    ['Channel', selectedMapping.channel],
                    ['Chat ID', selectedMapping.chatId],
                    ['Workspace', selectedMapping.workspace],
                    ['Label', selectedMapping.label || '—'],
                    ['Mode', selectedMapping.mode || 'cloud'],
                    ['Senders', selectedMapping.allowedSenders?.join(', ') || 'All'],
                  ].map(([label, value]) => (
                    <div key={label as string}>
                      <p className="text-xs text-zinc-500 mb-0.5">{label as string}</p>
                      <p className="text-zinc-300 font-mono text-xs break-all">{value as string}</p>
                    </div>
                  ))}
                  {selectedMapping.localBackend && (
                    <>
                      <div>
                        <p className="text-xs text-zinc-500 mb-0.5">Local Backend</p>
                        <p className="text-zinc-300 font-mono text-xs">{selectedMapping.localBackend}</p>
                      </div>
                      <div>
                        <p className="text-xs text-zinc-500 mb-0.5">Local Model</p>
                        <p className="text-zinc-300 font-mono text-xs">{selectedMapping.localModel || '—'}</p>
                      </div>
                    </>
                  )}
                </div>
              ) : (
                <p className="text-sm text-zinc-500">Select a mapping to view details.</p>
              )}
            </Card>
          </div>
        </div>
      </main>

      <ConfirmDialog
        open={!!deleteMapping}
        title="Remove Mapping"
        message={`Remove routing for "${deleteMapping?.label || deleteMapping?.chatId}"?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemove}
        onCancel={() => setDeleteMapping(null)}
      />
    </>
  );
}
