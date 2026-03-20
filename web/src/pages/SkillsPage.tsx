import { useEffect, useMemo, useState } from 'react';
import {
  FolderTree, Loader2, Puzzle, RefreshCw, Search, Plus, Trash2, Boxes, PackageOpen, ShieldCheck,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import { getSkills, installSkill, removeSkill, type Skill, type SkillsData } from '../lib/api';

function sourceBadge(source: string) {
  switch (source) {
    case 'workspace':
      return <StatusBadge variant="ready">workspace</StatusBadge>;
    case 'global':
      return <StatusBadge variant="warning">global</StatusBadge>;
    case 'builtin':
      return <StatusBadge variant="info">builtin</StatusBadge>;
    default:
      return <StatusBadge variant="muted">{source}</StatusBadge>;
  }
}

function sourceDescription(source: string) {
  switch (source) {
    case 'workspace':
      return 'Project-local override. Safe to remove from this UI.';
    case 'global':
      return 'User-level fallback shared across workspaces.';
    case 'builtin':
      return 'Bundled fallback resolved from the baseline skill source.';
    default:
      return 'Unknown source.';
  }
}

export default function SkillsPage() {
  const [data, setData] = useState<SkillsData | null>(null);
  const [selectedName, setSelectedName] = useState('');
  const [query, setQuery] = useState('');
  const [installPath, setInstallPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [deleteSkill, setDeleteSkill] = useState<Skill | null>(null);
  const [flash, setFlash] = useState('');
  const [error, setError] = useState('');

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const fetchData = async () => {
    setLoading(true);
    try {
      const next = await getSkills();
      setData(next);
      setError('');
      setSelectedName((current) => {
        if (current && next.installed.some((skill) => skill.name === current)) {
          return current;
        }
        return next.installed[0]?.name || '';
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load skills');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const filteredSkills = useMemo(() => {
    const skills = data?.installed ?? [];
    const term = query.trim().toLowerCase();
    if (!term) return skills;
    return skills.filter((skill) =>
      skill.name.toLowerCase().includes(term)
      || skill.source.toLowerCase().includes(term)
      || (skill.description || '').toLowerCase().includes(term)
      || skill.path.toLowerCase().includes(term));
  }, [data, query]);

  const selectedSkill = useMemo(() => {
    if (filteredSkills.length === 0) {
      return data?.installed.find((skill) => skill.name === selectedName) ?? null;
    }
    return filteredSkills.find((skill) => skill.name === selectedName) ?? filteredSkills[0] ?? null;
  }, [filteredSkills, data?.installed, selectedName]);

  const handleInstall = async () => {
    const path = installPath.trim();
    if (!path) return;
    setInstalling(true);
    try {
      await installSkill(path);
      showFlash(`Installed ${path}`);
      setInstallPath('');
      await fetchData();
      setSelectedName(path.split('/').filter(Boolean).pop() || '');
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setInstalling(false);
    }
  };

  const handleRemove = async () => {
    if (!deleteSkill) return;
    try {
      await removeSkill(deleteSkill.name);
      showFlash(`Removed ${deleteSkill.name}`);
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setDeleteSkill(null);
    }
  };

  const counts = data?.counts ?? { total: 0, workspace: 0, global: 0, builtin: 0 };

  return (
    <>
      <TopBar title="Skills" />
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

        <section className="rounded-2xl border border-border bg-surface-100 px-5 py-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-3xl space-y-1.5">
              <p className="text-[11px] font-semibold uppercase tracking-[0.24em] text-brand/80">Skill Resolution</p>
              <h2 className="text-xl font-semibold text-zinc-100">Resolved skills, source precedence, and workspace installs</h2>
              <p className="text-sm text-zinc-500">
                This module shows the actual resolved skill set the agent can see right now. Resolution order is
                workspace first, then global fallback, then bundled builtin fallback.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {data?.sourcePriority.map((source) => (
                <span key={source}>{sourceBadge(source)}</span>
              ))}
            </div>
          </div>
        </section>

        <section className="grid gap-3 xl:grid-cols-4">
          {[
            { label: 'Resolved', value: counts.total, note: 'Skills visible to the agent right now', icon: Puzzle, variant: 'ready' as const },
            { label: 'Workspace', value: counts.workspace, note: 'Project-local overrides in this workspace', icon: FolderTree, variant: 'ready' as const },
            { label: 'Global', value: counts.global, note: 'User-level fallback skills', icon: Boxes, variant: 'warning' as const },
            { label: 'Builtin', value: counts.builtin, note: 'Bundled baseline fallback skills', icon: PackageOpen, variant: 'info' as const },
          ].map((item) => {
            const Icon = item.icon;
            return (
              <div key={item.label} className="rounded-xl border border-border bg-surface-100 px-4 py-3">
                <div className="flex items-center justify-between">
                  <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{item.label}</p>
                  <Icon className="h-4 w-4 text-zinc-500" />
                </div>
                <div className="mt-2 flex items-baseline justify-between gap-2">
                  <span className="text-2xl font-semibold text-zinc-100">{item.value}</span>
                  <StatusBadge variant={item.variant}>{item.label}</StatusBadge>
                </div>
                <p className="mt-2 text-xs text-zinc-500">{item.note}</p>
              </div>
            );
          })}
        </section>

        <section className="grid gap-5 xl:grid-cols-[minmax(0,1.7fr)_380px]">
          <Card
            title={`Installed Skills (${data?.installed.length ?? 0})`}
            className="overflow-hidden"
            actions={
              <div className="flex items-center gap-2">
                <div className="relative hidden md:block">
                  <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-600" />
                  <input
                    type="text"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search skills"
                    className="w-56 rounded-md border border-border bg-surface-100 py-2 pl-8 pr-3 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                  />
                </div>
                <button
                  onClick={fetchData}
                  disabled={loading}
                  className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors disabled:opacity-50"
                  title="Refresh"
                >
                  {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
                </button>
              </div>
            }
          >
            <div className="mb-4 md:hidden">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-zinc-600" />
                <input
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search skills"
                  className="w-full rounded-md border border-border bg-surface-100 py-2 pl-8 pr-3 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                />
              </div>
            </div>

            {loading && !data ? (
              <div className="space-y-2">
                {[1, 2, 3, 4].map((i) => (
                  <div key={i} className="h-12 rounded-md bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : data && data.installed.length === 0 ? (
              <EmptyState
                icon={Puzzle}
                title="No resolved skills"
                description="Install a workspace skill to make it available immediately. Global and builtin fallbacks will also appear here when present."
              />
            ) : filteredSkills.length === 0 ? (
              <EmptyState
                icon={Search}
                title="No skills match this filter"
                description="Clear the search to see the full resolved skill set."
              />
            ) : (
              <div className="overflow-x-auto rounded-md border border-border-subtle bg-surface-50/20">
                <table className="w-full table-fixed">
                  <thead>
                    <tr className="border-b border-border bg-surface-50/40">
                      <th className="w-48 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Skill</th>
                      <th className="w-28 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Source</th>
                      <th className="text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Description</th>
                      <th className="w-64 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Path</th>
                      <th className="w-20 px-3 py-2 text-right text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {filteredSkills.map((skill) => {
                      const selected = selectedSkill?.name === skill.name;
                      return (
                        <tr
                          key={skill.name}
                          onClick={() => setSelectedName(skill.name)}
                          className={`cursor-pointer transition-colors ${selected ? 'bg-brand/5' : 'hover:bg-surface-50/45'}`}
                        >
                          <td className="px-3 py-2 align-top">
                            <div className="flex items-center gap-2">
                              <Puzzle className={`mt-0.5 h-4 w-4 flex-shrink-0 ${selected ? 'text-brand' : 'text-zinc-600'}`} />
                              <div className="min-w-0">
                                <p className="truncate text-sm font-medium text-zinc-100">{skill.name}</p>
                                <p className="truncate text-xs text-zinc-500">{sourceDescription(skill.source)}</p>
                              </div>
                            </div>
                          </td>
                          <td className="px-3 py-2 align-top">{sourceBadge(skill.source)}</td>
                          <td className="px-3 py-2 align-top text-sm leading-relaxed text-zinc-400">
                            <div className="line-clamp-2">{skill.description || 'No description available.'}</div>
                          </td>
                          <td className="px-3 py-2 align-top">
                            <div className="truncate text-[12px] text-zinc-500 font-mono">{skill.path}</div>
                          </td>
                          <td className="px-3 py-2 align-top">
                            <div className="flex items-center justify-end gap-1">
                              <button
                                onClick={(e) => {
                                  e.stopPropagation();
                                  if (skill.canRemove) setDeleteSkill(skill);
                                }}
                                disabled={!skill.canRemove}
                                className={`inline-flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
                                  skill.canRemove
                                    ? 'border-transparent text-zinc-600 hover:border-red-500/25 hover:bg-red-500/10 hover:text-red-400'
                                    : 'cursor-not-allowed border-transparent text-zinc-700'
                                }`}
                                title={skill.canRemove ? `Remove ${skill.name}` : 'Only workspace-installed skills can be removed here'}
                              >
                                <Trash2 className="w-3.5 h-3.5" />
                              </button>
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <div className="space-y-5">
            <Card title="Install Skill">
              <div className="space-y-4">
                <p className="text-sm text-zinc-500">
                  Install a workspace skill from a GitHub repo path. Workspace skills take precedence over global and builtin fallbacks.
                </p>
                <label className="space-y-1.5 block">
                  <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">GitHub Repo Path</span>
                  <input
                    type="text"
                    value={installPath}
                    onChange={(e) => setInstallPath(e.target.value)}
                    placeholder="drpedapati/sciclaw-skills/weather"
                    className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleInstall();
                    }}
                  />
                </label>
                <div className="rounded-md border border-border-subtle bg-surface-50/20 px-3 py-3">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-zinc-500">Examples</p>
                  <div className="mt-2 space-y-1 text-[12px] text-zinc-400 font-mono">
                    <p>drpedapati/sciclaw-skills/weather</p>
                    <p>drpedapati/sciclaw-skills/pubmed-cli</p>
                  </div>
                </div>
                <button
                  onClick={handleInstall}
                  disabled={installing || !installPath.trim()}
                  className="inline-flex items-center gap-2 rounded-md bg-brand px-3 py-2 text-sm font-medium text-surface-500 transition-colors hover:bg-brand-500 disabled:opacity-50"
                >
                  {installing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                  Install into workspace
                </button>
              </div>
            </Card>

            <Card title="Skill Details">
              {selectedSkill ? (
                <div className="space-y-4">
                  <div>
                    <p className="text-[11px] uppercase tracking-[0.16em] text-zinc-500">Selected</p>
                    <div className="mt-2 flex items-center gap-2">
                      <span className="text-lg font-semibold text-zinc-100">{selectedSkill.name}</span>
                      {sourceBadge(selectedSkill.source)}
                    </div>
                  </div>
                  <div className="rounded-md border border-border-subtle bg-surface-50/20 px-3 py-3">
                    <p className="text-[11px] uppercase tracking-[0.16em] text-zinc-500">Description</p>
                    <p className="mt-2 text-sm leading-relaxed text-zinc-400">
                      {selectedSkill.description || 'No description available.'}
                    </p>
                  </div>
                  <div className="space-y-3">
                    <div>
                      <p className="text-[11px] uppercase tracking-[0.16em] text-zinc-500">Path</p>
                      <p className="mt-1 break-all font-mono text-[12px] text-zinc-400">{selectedSkill.path}</p>
                    </div>
                    <div>
                      <p className="text-[11px] uppercase tracking-[0.16em] text-zinc-500">Source Behavior</p>
                      <p className="mt-1 text-sm text-zinc-400">{sourceDescription(selectedSkill.source)}</p>
                    </div>
                    <div className="flex items-center justify-between rounded-md border border-border-subtle bg-surface-50/20 px-3 py-3">
                      <div>
                        <p className="text-sm font-medium text-zinc-200">Removal</p>
                        <p className="mt-1 text-xs text-zinc-500">
                          {selectedSkill.canRemove
                            ? 'This is a workspace-installed skill and can be removed here.'
                            : 'This is a fallback source. Remove or override it outside this workspace if needed.'}
                        </p>
                      </div>
                      <StatusBadge variant={selectedSkill.canRemove ? 'ready' : 'muted'}>
                        {selectedSkill.canRemove ? 'Workspace-managed' : 'Read-only here'}
                      </StatusBadge>
                    </div>
                  </div>
                </div>
              ) : (
                <EmptyState
                  icon={ShieldCheck}
                  title="No skill selected"
                  description="Select a skill from the resolved table to inspect its path, source, and management options."
                />
              )}
            </Card>

            <Card title="Resolution Paths">
              <div className="space-y-3 text-sm">
                {[
                  { label: 'Workspace', value: data?.workspaceSkillsDir || '—', variant: 'ready' as const },
                  { label: 'Global', value: data?.globalSkillsDir || '—', variant: 'warning' as const },
                  { label: 'Builtin', value: data?.builtinSkillsDir || 'No builtin source found', variant: data?.builtinSkillsDir ? 'info' as const : 'muted' as const },
                ].map((item) => (
                  <div key={item.label} className="rounded-md border border-border-subtle bg-surface-50/20 px-3 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <p className="text-sm font-medium text-zinc-200">{item.label}</p>
                      <StatusBadge variant={item.variant}>{item.label}</StatusBadge>
                    </div>
                    <p className="mt-2 break-all font-mono text-[12px] text-zinc-500">{item.value}</p>
                  </div>
                ))}
              </div>
            </Card>
          </div>
        </section>
      </main>

      <ConfirmDialog
        open={!!deleteSkill}
        title="Remove Skill"
        message={`Remove workspace skill "${deleteSkill?.name}"?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemove}
        onCancel={() => setDeleteSkill(null)}
      />
    </>
  );
}
