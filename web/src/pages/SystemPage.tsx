import { useEffect, useMemo, useState } from 'react';
import {
  FileCode2, FileText, Loader2, RefreshCw, Edit3, Save, X, GitCompare, RotateCcw,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import {
  getSystemFiles,
  saveSystemFile,
  resetSystemFile,
  type WorkspaceFileInfo,
  type SystemFilesResponse,
} from '../lib/api';

// ── Diff helper (LCS-based line diff, no external deps) ──

type DiffLine = { kind: 'common' | 'added' | 'removed'; text: string };

function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split('\n');
  const b = after.split('\n');

  // Build LCS table
  const m = a.length;
  const n = b.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      if (a[i] === b[j]) {
        dp[i][j] = 1 + dp[i + 1][j + 1];
      } else {
        dp[i][j] = Math.max(dp[i + 1][j], dp[i][j + 1]);
      }
    }
  }

  // Walk back through table to build diff
  const result: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      result.push({ kind: 'common', text: a[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      result.push({ kind: 'removed', text: a[i] });
      i++;
    } else {
      result.push({ kind: 'added', text: b[j] });
      j++;
    }
  }
  while (i < m) { result.push({ kind: 'removed', text: a[i++] }); }
  while (j < n) { result.push({ kind: 'added', text: b[j++] }); }
  return result;
}

// ── Status helpers ──

function statusVariant(status: WorkspaceFileInfo['status']) {
  switch (status) {
    case 'current': return 'ready' as const;
    case 'customized': return 'warning' as const;
    case 'missing': return 'error' as const;
  }
}

function statusLabel(status: WorkspaceFileInfo['status']) {
  switch (status) {
    case 'current': return 'current';
    case 'customized': return 'customized';
    case 'missing': return 'missing';
  }
}

// ── Main component ──

export default function SystemPage() {
  const [data, setData] = useState<SystemFilesResponse | null>(null);
  const [selectedPath, setSelectedPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [flash, setFlash] = useState('');
  const [error, setError] = useState('');
  const [activeWorkspace, setActiveWorkspace] = useState('');
  const [editMode, setEditMode] = useState(false);
  const [editContent, setEditContent] = useState('');
  const [saving, setSaving] = useState(false);
  const [showDiff, setShowDiff] = useState(false);
  const [resetTarget, setResetTarget] = useState<WorkspaceFileInfo | null>(null);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const fetchData = async (workspace?: string) => {
    setLoading(true);
    setEditMode(false);
    setShowDiff(false);
    try {
      const ws = workspace || activeWorkspace || undefined;
      const next = await getSystemFiles(ws);
      setData(next);
      setActiveWorkspace(next.activeWorkspace);
      setError('');
      setSelectedPath((current) => {
        if (current && next.files.some((f) => f.relativePath === current)) return current;
        return next.files[0]?.relativePath || '';
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load system files');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const selectedFile = useMemo(
    () => data?.files.find((f) => f.relativePath === selectedPath) ?? null,
    [data, selectedPath],
  );

  const handleSelectFile = (rel: string) => {
    setSelectedPath(rel);
    setEditMode(false);
    setShowDiff(false);
  };

  const handleEdit = () => {
    if (!selectedFile) return;
    setEditContent(selectedFile.content || '');
    setEditMode(true);
    setShowDiff(false);
  };

  const handleCancelEdit = () => {
    setEditMode(false);
    setEditContent('');
  };

  const handleSave = async () => {
    if (!selectedFile || !data) return;
    setSaving(true);
    try {
      await saveSystemFile(data.activeWorkspace, selectedFile.relativePath, editContent);
      showFlash(`Saved ${selectedFile.name}`);
      setEditMode(false);
      setShowDiff(false);
      await fetchData(data.activeWorkspace);
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setSaving(false);
    }
  };

  const handleReset = async () => {
    if (!resetTarget || !data) return;
    try {
      await resetSystemFile(data.activeWorkspace, resetTarget.relativePath);
      showFlash(`Reset ${resetTarget.name} to template default`);
      await fetchData(data.activeWorkspace);
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setResetTarget(null);
    }
  };

  const handleWorkspaceChange = (ws: string) => {
    setActiveWorkspace(ws);
    fetchData(ws);
  };

  // Build workspace options for the dropdown
  const workspaceOptions = useMemo(() => {
    if (!data) return [];
    const opts: { value: string; label: string }[] = [];
    if (data.primaryWorkspace) {
      opts.push({ value: data.primaryWorkspace, label: `Primary — ${data.primaryWorkspace}` });
    }
    if (data.sharedWorkspace && data.sharedWorkspace !== data.primaryWorkspace) {
      opts.push({ value: data.sharedWorkspace, label: `Shared — ${data.sharedWorkspace}` });
    }
    for (const rw of data.routingWorkspaces) {
      if (!opts.some((o) => o.value === rw.workspace)) {
        opts.push({ value: rw.workspace, label: `${rw.label} — ${rw.workspace}` });
      }
    }
    return opts;
  }, [data]);

  const diffLines = useMemo(() => {
    if (!showDiff || !selectedFile) return [];
    const before = selectedFile.templateContent || '';
    const after = editMode ? editContent : selectedFile.content || '';
    return lineDiff(before, after);
  }, [showDiff, selectedFile, editMode, editContent]);

  const counts = useMemo(() => {
    const files = data?.files ?? [];
    return {
      current: files.filter((f) => f.status === 'current').length,
      customized: files.filter((f) => f.status === 'customized').length,
      missing: files.filter((f) => f.status === 'missing').length,
    };
  }, [data]);

  return (
    <>
      <TopBar title="System" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md border border-brand/20 bg-brand/10 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}
        {error && (
          <div className="rounded-md border border-red-500/20 bg-red-500/10 px-4 py-2 text-sm text-red-300 animate-fade-in">
            {error}
          </div>
        )}

        {/* Header section */}
        <section className="rounded-2xl border border-border bg-surface-100 px-5 py-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-3xl space-y-1.5">
              <p className="text-[11px] font-semibold uppercase tracking-[0.24em] text-brand/80">Personality Files</p>
              <h2 className="text-xl font-semibold text-zinc-100">Workspace bootstrap files and template reconciliation</h2>
              <p className="text-sm text-zinc-500">
                View, edit, and diff the 7 personality files that bootstrap every agent session. Compare local files
                against the bundled release templates to detect drift.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge variant="ready">{counts.current} current</StatusBadge>
              <StatusBadge variant="warning">{counts.customized} customized</StatusBadge>
              {counts.missing > 0 && (
                <StatusBadge variant="error">{counts.missing} missing</StatusBadge>
              )}
            </div>
          </div>
        </section>

        {/* Workspace switcher */}
        {workspaceOptions.length > 1 && (
          <div className="flex items-center gap-3">
            <label className="text-xs uppercase tracking-[0.16em] text-zinc-500">Workspace</label>
            <select
              value={activeWorkspace}
              onChange={(e) => handleWorkspaceChange(e.target.value)}
              className="rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 focus:outline-none focus:ring-1 focus:ring-brand/40"
            >
              {workspaceOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          </div>
        )}

        {/* Two-column layout */}
        <section className="grid gap-5 xl:grid-cols-[minmax(0,1.7fr)_380px]">
          {/* Left: file list */}
          <Card
            title={`Personality Files (${data?.files.length ?? 0})`}
            className="overflow-hidden"
            actions={
              <button
                onClick={() => fetchData(activeWorkspace)}
                disabled={loading}
                className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors disabled:opacity-50"
                title="Refresh"
              >
                {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
              </button>
            }
          >
            {loading && !data ? (
              <div className="space-y-2">
                {[1, 2, 3, 4, 5, 6, 7].map((i) => (
                  <div key={i} className="h-12 rounded-md bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : (
              <div className="overflow-x-auto rounded-md border border-border-subtle bg-surface-50/20">
                <table className="w-full table-fixed">
                  <thead>
                    <tr className="border-b border-border bg-surface-50/40">
                      <th className="text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">File</th>
                      <th className="w-28 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Status</th>
                      <th className="w-24 text-right px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Size</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {(data?.files ?? []).map((file) => {
                      const selected = selectedPath === file.relativePath;
                      return (
                        <tr
                          key={file.relativePath}
                          onClick={() => handleSelectFile(file.relativePath)}
                          className={`cursor-pointer transition-colors ${selected ? 'bg-brand/5' : 'hover:bg-surface-50/45'}`}
                        >
                          <td className="px-3 py-2.5 align-middle">
                            <div className="flex items-center gap-2">
                              <FileText className={`h-4 w-4 flex-shrink-0 ${selected ? 'text-brand' : 'text-zinc-600'}`} />
                              <div className="min-w-0">
                                <p className="truncate text-sm font-medium text-zinc-100">{file.name}</p>
                                {file.relativePath !== file.name && (
                                  <p className="truncate text-xs text-zinc-600 font-mono">{file.relativePath}</p>
                                )}
                              </div>
                            </div>
                          </td>
                          <td className="px-3 py-2.5 align-middle">
                            <StatusBadge variant={statusVariant(file.status)}>
                              {statusLabel(file.status)}
                            </StatusBadge>
                          </td>
                          <td className="px-3 py-2.5 align-middle text-right">
                            <span className="text-xs text-zinc-500 font-mono">
                              {file.status === 'missing' ? '—' : `${(file.size / 1024).toFixed(1)}k`}
                            </span>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          {/* Right: detail panel */}
          <div className="space-y-5">
            {selectedFile ? (
              <Card
                title={selectedFile.name}
                className="overflow-hidden"
                actions={
                  <StatusBadge variant={statusVariant(selectedFile.status)}>
                    {statusLabel(selectedFile.status)}
                  </StatusBadge>
                }
              >
                <div className="space-y-4">
                  {/* Meta */}
                  <div className="space-y-2 text-xs text-zinc-500">
                    <div className="flex items-start gap-2">
                      <span className="uppercase tracking-[0.14em] text-zinc-600 w-16 flex-shrink-0">Path</span>
                      <span className="font-mono break-all text-zinc-400">{selectedFile.absolutePath}</span>
                    </div>
                    {selectedFile.modTime && (
                      <div className="flex items-center gap-2">
                        <span className="uppercase tracking-[0.14em] text-zinc-600 w-16 flex-shrink-0">Modified</span>
                        <span className="text-zinc-400">{new Date(selectedFile.modTime).toLocaleString()}</span>
                      </div>
                    )}
                  </div>

                  {/* Action bar */}
                  {selectedFile.status !== 'missing' && (
                    <div className="flex flex-wrap items-center gap-2">
                      {!editMode ? (
                        <button
                          onClick={handleEdit}
                          className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs text-zinc-300 hover:bg-surface-50 transition-colors"
                        >
                          <Edit3 className="w-3.5 h-3.5" />
                          Edit
                        </button>
                      ) : (
                        <>
                          <button
                            onClick={handleSave}
                            disabled={saving}
                            className="inline-flex items-center gap-1.5 rounded-md bg-brand px-3 py-1.5 text-xs font-medium text-surface-500 hover:bg-brand-500 transition-colors disabled:opacity-50"
                          >
                            {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
                            Save
                          </button>
                          <button
                            onClick={handleCancelEdit}
                            className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs text-zinc-300 hover:bg-surface-50 transition-colors"
                          >
                            <X className="w-3.5 h-3.5" />
                            Cancel
                          </button>
                        </>
                      )}
                      {selectedFile.hasTemplate && (
                        <button
                          onClick={() => setShowDiff((v) => !v)}
                          className={`inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs transition-colors ${
                            showDiff
                              ? 'border-brand/40 bg-brand/10 text-brand'
                              : 'border-border text-zinc-300 hover:bg-surface-50'
                          }`}
                        >
                          <GitCompare className="w-3.5 h-3.5" />
                          {showDiff ? 'Hide Diff' : 'Show Diff'}
                        </button>
                      )}
                      {selectedFile.hasTemplate && (
                        <button
                          onClick={() => setResetTarget(selectedFile)}
                          className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs text-zinc-300 hover:border-red-500/25 hover:bg-red-500/10 hover:text-red-400 transition-colors"
                        >
                          <RotateCcw className="w-3.5 h-3.5" />
                          Reset to Default
                        </button>
                      )}
                    </div>
                  )}

                  {selectedFile.status === 'missing' && selectedFile.hasTemplate && (
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => setResetTarget(selectedFile)}
                        className="inline-flex items-center gap-1.5 rounded-md bg-brand px-3 py-1.5 text-xs font-medium text-surface-500 hover:bg-brand-500 transition-colors"
                      >
                        <RotateCcw className="w-3.5 h-3.5" />
                        Create from Template
                      </button>
                    </div>
                  )}

                  {/* Diff view */}
                  {showDiff && diffLines.length > 0 && (
                    <div className="rounded-md border border-border-subtle overflow-hidden">
                      <div className="bg-surface-50/40 border-b border-border-subtle px-3 py-1.5 flex items-center gap-2">
                        <GitCompare className="w-3.5 h-3.5 text-zinc-500" />
                        <span className="text-[11px] uppercase tracking-[0.16em] text-zinc-500">
                          Template vs {editMode ? 'Edited' : 'Local'}
                        </span>
                      </div>
                      <div className="overflow-auto max-h-80">
                        <pre className="text-[11px] font-mono leading-5">
                          {diffLines.map((line, idx) => (
                            <div
                              key={idx}
                              className={
                                line.kind === 'added'
                                  ? 'bg-brand/10 text-brand px-3'
                                  : line.kind === 'removed'
                                    ? 'bg-red-500/10 text-red-400 px-3'
                                    : 'text-zinc-500 px-3'
                              }
                            >
                              <span className="select-none mr-2 text-zinc-600">
                                {line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}
                              </span>
                              {line.text}
                            </div>
                          ))}
                        </pre>
                      </div>
                    </div>
                  )}

                  {/* Content viewer / editor */}
                  {selectedFile.status === 'missing' ? (
                    <div className="rounded-md border border-border-subtle bg-surface-50/20 px-4 py-6 text-center">
                      <FileCode2 className="w-8 h-8 text-zinc-600 mx-auto mb-2" />
                      <p className="text-sm text-zinc-500">This file does not exist in the workspace.</p>
                      {selectedFile.hasTemplate && (
                        <p className="text-xs text-zinc-600 mt-1">A template is available — use "Create from Template" above.</p>
                      )}
                    </div>
                  ) : editMode ? (
                    <textarea
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      className="w-full rounded-md border border-border bg-surface-50/20 px-3 py-3 text-[12px] font-mono text-zinc-300 leading-5 focus:outline-none focus:ring-1 focus:ring-brand/40 resize-none"
                      rows={20}
                      spellCheck={false}
                    />
                  ) : (
                    <div className="overflow-auto max-h-[480px] rounded-md border border-border-subtle bg-surface-50/20">
                      <pre className="px-3 py-3 text-[12px] font-mono text-zinc-400 leading-5 whitespace-pre-wrap break-words">
                        {selectedFile.content || ''}
                      </pre>
                    </div>
                  )}
                </div>
              </Card>
            ) : (
              <Card title="File Details">
                <div className="flex flex-col items-center justify-center py-10 text-center gap-3">
                  <FileCode2 className="w-8 h-8 text-zinc-600" />
                  <p className="text-sm text-zinc-500">Select a file from the list to view its content.</p>
                </div>
              </Card>
            )}
          </div>
        </section>
      </main>

      <ConfirmDialog
        open={!!resetTarget}
        title={resetTarget?.status === 'missing' ? 'Create from Template' : 'Reset to Default'}
        message={
          resetTarget?.status === 'missing'
            ? `Create "${resetTarget?.name}" from the bundled release template? This will create the file in the current workspace.`
            : `Reset "${resetTarget?.name}" to the bundled release template? All local edits will be lost.`
        }
        confirmLabel={resetTarget?.status === 'missing' ? 'Create' : 'Reset'}
        danger={resetTarget?.status !== 'missing'}
        onConfirm={handleReset}
        onCancel={() => setResetTarget(null)}
      />
    </>
  );
}
