import { useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle,
  Clock3,
  GitBranch,
  Loader2,
  RefreshCw,
  Search,
  Users,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import EmptyState from '../components/EmptyState';
import { getJobs, type JobRecord, type JobsResponse } from '../lib/api';

function formatRelative(ms: number) {
  if (!ms) return '—';
  const delta = Math.max(0, Date.now() - ms);
  const seconds = Math.floor(delta / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function formatDuration(seconds: number) {
  if (!seconds) return '0s';
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const rem = seconds % 60;
  if (minutes < 60) return rem ? `${minutes}m ${rem}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const min = minutes % 60;
  return min ? `${hours}h ${min}m` : `${hours}h`;
}

function compactPath(path: string) {
  if (!path) return '—';
  const parts = path.split('/').filter(Boolean);
  if (parts.length <= 3) return path;
  return `…/${parts.slice(-3).join('/')}`;
}

function requesterLabel(job: JobRecord) {
  if (job.userName) return job.userName;
  if (job.userId) return job.userId;
  return 'Unknown';
}

function targetLabel(job: JobRecord) {
  if (job.routeLabel) return job.routeLabel;
  if (job.chatId) return job.chatId;
  return job.workspace || 'Unscoped';
}

function stateVariant(state: string) {
  switch (state) {
    case 'running': return 'ready' as const;
    case 'queued': return 'warning' as const;
    case 'failed': return 'error' as const;
    case 'interrupted': return 'warning' as const;
    case 'cancelled': return 'muted' as const;
    case 'done': return 'info' as const;
    default: return 'muted' as const;
  }
}

function laneLabel(job: JobRecord) {
  return job.lane === 'btw' ? '/btw' : 'main';
}

type TargetSummary = {
  key: string;
  label: string;
  channel: string;
  chatId: string;
  workspace: string;
  active: number;
  running: number;
  queued: number;
  failed: number;
  users: string[];
  updatedAt: number;
};

type PersonSummary = {
  key: string;
  name: string;
  active: number;
  total: number;
  failed: number;
  updatedAt: number;
};

export default function JobsPage() {
  const [data, setData] = useState<JobsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState<'all' | 'active' | 'running' | 'queued' | 'failed' | 'done'>('all');
  const [channelFilter, setChannelFilter] = useState<'all' | 'discord' | 'telegram'>('all');
  const [laneFilter, setLaneFilter] = useState<'all' | 'main' | 'btw'>('all');
  const [selectedTarget, setSelectedTarget] = useState('');
  const [selectedJobId, setSelectedJobId] = useState('');

  const fetchData = async () => {
    setLoading(true);
    try {
      const next = await getJobs();
      setData(next);
      setError('');
      if (!selectedJobId && next.jobs.length > 0) {
        setSelectedJobId(next.jobs[0].id);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load jobs');
    } finally {
      setLoading(false);
      setLoaded(true);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const jobs = data?.jobs ?? [];
  const activeJobs = useMemo(() => jobs.filter((job) => job.state === 'running' || job.state === 'queued'), [jobs]);

  const targetGroups = useMemo<TargetSummary[]>(() => {
    const grouped = new Map<string, TargetSummary>();
    for (const job of activeJobs) {
      const key = job.targetKey || `${job.channel}:${job.chatId}:${job.workspace}`;
      const existing = grouped.get(key) ?? {
        key,
        label: targetLabel(job),
        channel: job.channel,
        chatId: job.chatId,
        workspace: job.workspace,
        active: 0,
        running: 0,
        queued: 0,
        failed: 0,
        users: [],
        updatedAt: job.updatedAt,
      };
      if (job.state === 'running' || job.state === 'queued') existing.active += 1;
      if (job.state === 'running') existing.running += 1;
      if (job.state === 'queued') existing.queued += 1;
      if (job.state === 'failed' || job.state === 'interrupted') existing.failed += 1;
      if (job.updatedAt > existing.updatedAt) existing.updatedAt = job.updatedAt;
      const requester = requesterLabel(job);
      if (!existing.users.includes(requester) && requester !== 'Unknown') existing.users.push(requester);
      grouped.set(key, existing);
    }
    return Array.from(grouped.values()).sort((a, b) => {
      if (a.active !== b.active) return b.active - a.active;
      if (a.failed !== b.failed) return b.failed - a.failed;
      return b.updatedAt - a.updatedAt;
    });
  }, [activeJobs]);

  const people = useMemo<PersonSummary[]>(() => {
    const grouped = new Map<string, PersonSummary>();
    for (const job of jobs) {
      const key = job.userId || job.userName || 'unknown';
      const name = requesterLabel(job);
      const existing = grouped.get(key) ?? { key, name, active: 0, total: 0, failed: 0, updatedAt: job.updatedAt };
      existing.total += 1;
      if (job.state === 'running' || job.state === 'queued') existing.active += 1;
      if (job.state === 'failed' || job.state === 'interrupted') existing.failed += 1;
      if (job.updatedAt > existing.updatedAt) existing.updatedAt = job.updatedAt;
      grouped.set(key, existing);
    }
    return Array.from(grouped.values())
      .filter((person) => person.key !== 'unknown' || person.total > 1)
      .sort((a, b) => {
        if (a.active !== b.active) return b.active - a.active;
        if (a.failed !== b.failed) return b.failed - a.failed;
        return b.total - a.total;
      })
      .slice(0, 8);
  }, [jobs]);

  const channels = useMemo(() => Array.from(new Set(jobs.map((job) => job.channel).filter(Boolean))), [jobs]);

  const filteredJobs = useMemo(() => {
    const term = search.trim().toLowerCase();
    return jobs.filter((job) => {
      if (stateFilter === 'active' && job.state !== 'running' && job.state !== 'queued') return false;
      if (stateFilter !== 'all' && stateFilter !== 'active' && job.state !== stateFilter) return false;
      if (channelFilter !== 'all' && job.channel !== channelFilter) return false;
      if (laneFilter !== 'all' && job.lane !== laneFilter) return false;
      if (selectedTarget && (job.targetKey || `${job.channel}:${job.chatId}:${job.workspace}`) !== selectedTarget) return false;
      if (!term) return true;
      const haystack = [
        job.id,
        job.shortId,
        job.askSummary,
        job.detail,
        job.userName,
        job.userId,
        job.workspace,
        job.routeLabel,
        job.chatId,
        job.lastError,
      ].join(' ').toLowerCase();
      return haystack.includes(term);
    });
  }, [jobs, search, stateFilter, channelFilter, laneFilter, selectedTarget]);

  const selectedJob = filteredJobs.find((job) => job.id === selectedJobId) ?? filteredJobs[0] ?? jobs.find((job) => job.id === selectedJobId) ?? jobs[0] ?? null;

  return (
    <>
      <TopBar title="Jobs" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {error && (
          <div className="rounded-md border border-red-500/20 bg-red-500/10 px-4 py-2 text-sm text-red-300 animate-fade-in">
            {error}
          </div>
        )}

        <section className="rounded-2xl border border-border bg-surface-100 px-5 py-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-3xl space-y-1.5">
              <p className="text-[11px] font-semibold uppercase tracking-[0.24em] text-brand/80">Read-Only Queue Ledger</p>
              <h2 className="text-xl font-semibold text-zinc-100">Current backlog, job IDs, and recent failures across Discord rooms</h2>
              <p className="text-sm text-zinc-500">
                This page is operator visibility only. It reads the persisted job ledger the gateway writes and surfaces live queue pressure,
                IDs, and failure context. If a job needs to move, cancel it from Discord where the gateway owns the real scheduler state.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                onClick={fetchData}
                disabled={loading}
                className="inline-flex items-center gap-2 rounded-md border border-border px-3 py-2 text-xs font-medium text-zinc-300 transition-colors hover:bg-surface-50 disabled:opacity-60"
              >
                {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                Refresh ledger
              </button>
            </div>
          </div>
        </section>

        <section className="grid grid-cols-2 gap-3 xl:grid-cols-6">
          {[
            { label: 'Active jobs', value: data?.summary.active ?? 0, note: `${data?.summary.running ?? 0} running · ${data?.summary.queued ?? 0} queued`, variant: 'ready' as const },
            { label: 'Failures', value: (data?.summary.failed ?? 0) + (data?.summary.interrupted ?? 0), note: `${data?.summary.failed ?? 0} failed · ${data?.summary.interrupted ?? 0} interrupted`, variant: 'error' as const },
            { label: 'Completed', value: data?.summary.done ?? 0, note: `${data?.summary.cancelled ?? 0} cancelled`, variant: 'info' as const },
            { label: 'Channels', value: data?.summary.distinctChats ?? 0, note: `${data?.summary.distinctChannels ?? 0} channel types`, variant: 'muted' as const },
            { label: 'Requesters', value: data?.summary.distinctUsers ?? 0, note: 'Distinct users in ledger', variant: 'warning' as const },
            { label: 'Workspaces', value: data?.summary.distinctWorkspaces ?? 0, note: 'Ledger-backed view', variant: 'muted' as const },
          ].map((item) => (
            <div key={item.label} className="rounded-xl border border-border bg-surface-100 px-4 py-3">
              <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{item.label}</p>
              <div className="mt-2 flex items-baseline justify-between gap-2">
                <span className="text-2xl font-semibold text-zinc-100">{item.value}</span>
                <StatusBadge variant={item.variant}>{item.label.split(' ')[0]}</StatusBadge>
              </div>
              <p className="mt-2 text-xs text-zinc-500">{item.note}</p>
            </div>
          ))}
        </section>

        <section className="grid gap-5 xl:grid-cols-[320px_minmax(0,1fr)]">
          <div className="space-y-5">
            <Card title="Active Queue Pressure">
              {targetGroups.length === 0 ? (
                <EmptyState icon={GitBranch} title="No active backlog" description="Running and queued jobs will appear here when a room is actually under load." />
              ) : (
                <div className="space-y-2">
                  {targetGroups.slice(0, 10).map((target) => {
                    const isSelected = selectedTarget === target.key;
                    return (
                      <button
                        key={target.key}
                        onClick={() => setSelectedTarget((current) => current === target.key ? '' : target.key)}
                        className={`w-full rounded-lg border px-3 py-3 text-left transition-colors ${isSelected ? 'border-brand/30 bg-brand/8' : 'border-border-subtle bg-surface-50/20 hover:bg-surface-50/45'}`}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="flex items-center gap-2">
                              <StatusBadge variant={target.channel === 'discord' ? 'info' : 'ready'}>{target.channel}</StatusBadge>
                              {target.active > 0 ? <StatusBadge variant="warning">{target.active} active</StatusBadge> : null}
                            </div>
                            <p className="mt-2 truncate text-sm font-medium text-zinc-200">{target.label}</p>
                            <p className="mt-1 truncate text-[11px] text-zinc-500">{compactPath(target.workspace)}</p>
                          </div>
                          <div className="text-right text-[11px] text-zinc-500">
                            <p>{target.running} run</p>
                            <p>{target.queued} queue</p>
                          </div>
                        </div>
                        {target.users.length > 0 && (
                          <p className="mt-2 truncate text-[11px] text-zinc-600">{target.users.join(' · ')}</p>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </Card>

            <Card title="Top Requesters">
              {people.length === 0 ? (
                <p className="text-sm text-zinc-500">No requester metadata recorded yet.</p>
              ) : (
                <div className="space-y-2">
                  {people.map((person) => (
                    <div key={person.key} className="rounded-lg border border-border-subtle bg-surface-50/20 px-3 py-2.5">
                      <div className="flex items-center justify-between gap-3">
                        <div className="min-w-0">
                          <p className="truncate text-sm text-zinc-200">{person.name}</p>
                          <p className="text-[11px] text-zinc-500">{person.total} jobs · {formatRelative(person.updatedAt)}</p>
                        </div>
                        <div className="text-right text-[11px] text-zinc-500">
                          <p>{person.active} active</p>
                          <p>{person.failed} failed</p>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </div>

          <div className="space-y-5">
            <Card
              title={`Job Ledger (${filteredJobs.length})`}
              actions={
                <div className="flex flex-wrap items-center gap-2 text-[11px] text-zinc-500">
                  <span>{jobs.length} total</span>
                  {selectedTarget && (
                    <button onClick={() => setSelectedTarget('')} className="rounded-full border border-border px-2 py-0.5 text-zinc-400 hover:bg-surface-50 hover:text-zinc-200">
                      Clear lane focus
                    </button>
                  )}
                </div>
              }
            >
              <div className="space-y-4">
                <div className="grid gap-2 xl:grid-cols-[minmax(0,1.4fr)_140px_140px_140px]">
                  <label className="relative block">
                    <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-600" />
                    <input
                      value={search}
                      onChange={(e) => setSearch(e.target.value)}
                      placeholder="Search request, user, room, workspace, or error"
                      className="w-full rounded-md border border-border bg-surface-50/20 py-2 pl-9 pr-3 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    />
                  </label>
                  <select value={stateFilter} onChange={(e) => setStateFilter(e.target.value as typeof stateFilter)} className="rounded-md border border-border bg-surface-50/20 px-3 py-2 text-sm text-zinc-300 focus:outline-none focus:ring-1 focus:ring-brand/40">
                    <option value="all">All states</option>
                    <option value="active">Active only</option>
                    <option value="running">Running</option>
                    <option value="queued">Queued</option>
                    <option value="failed">Failed</option>
                    <option value="done">Done</option>
                  </select>
                  <select value={channelFilter} onChange={(e) => setChannelFilter(e.target.value as typeof channelFilter)} className="rounded-md border border-border bg-surface-50/20 px-3 py-2 text-sm text-zinc-300 focus:outline-none focus:ring-1 focus:ring-brand/40">
                    <option value="all">All channels</option>
                    {channels.map((channel) => (
                      <option key={channel} value={channel}>{channel}</option>
                    ))}
                  </select>
                  <select value={laneFilter} onChange={(e) => setLaneFilter(e.target.value as typeof laneFilter)} className="rounded-md border border-border bg-surface-50/20 px-3 py-2 text-sm text-zinc-300 focus:outline-none focus:ring-1 focus:ring-brand/40">
                    <option value="all">All lanes</option>
                    <option value="main">Main lane</option>
                    <option value="btw">/btw lane</option>
                  </select>
                </div>

                {!loaded ? (
                  <div className="space-y-2">
                    {[1, 2, 3, 4].map((i) => <div key={i} className="h-11 rounded-md bg-surface-50/30 animate-pulse" />)}
                  </div>
                ) : filteredJobs.length === 0 ? (
                  <EmptyState icon={Clock3} title="No jobs match this view" description="Broaden the filters or wait for the gateway to write more ledger entries." />
                ) : (
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface-50/15">
                    <div className="max-h-[30rem] overflow-auto">
                      <table className="w-full table-fixed">
                        <thead className="sticky top-0 z-10 bg-surface-200/98 backdrop-blur">
                          <tr className="border-b border-border">
                            <th className="w-40 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Job ID</th>
                            <th className="w-28 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">State</th>
                            <th className="w-20 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Lane</th>
                            <th className="px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Request</th>
                            <th className="w-32 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Requester</th>
                            <th className="w-36 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Room</th>
                            <th className="w-40 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Workspace</th>
                            <th className="w-28 px-3 py-2 text-left text-[10px] uppercase tracking-[0.18em] text-zinc-500">Updated</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-border-subtle">
                          {filteredJobs.map((job) => {
                            const isSelected = selectedJob?.id === job.id;
                            return (
                              <tr key={job.id} onClick={() => setSelectedJobId(job.id)} className={`cursor-pointer transition-colors ${isSelected ? 'bg-brand/7' : 'hover:bg-surface-50/40'}`}>
                                <td className="px-3 py-2 align-top">
                                  <div className="space-y-1.5">
                                    <p className="text-sm font-semibold text-zinc-100">{job.shortId || '—'}</p>
                                    <p className="truncate font-mono text-[11px] text-zinc-500">{job.id}</p>
                                  </div>
                                </td>
                                <td className="px-3 py-2 align-top">
                                  <div className="space-y-1">
                                    <StatusBadge variant={stateVariant(job.state)} dot>{job.state}</StatusBadge>
                                    {job.stale ? <p className="text-[11px] text-amber-300">stale</p> : <p className="text-[11px] text-zinc-600">{job.phase || '—'}</p>}
                                  </div>
                                </td>
                                <td className="px-3 py-2 align-top text-[12px] text-zinc-300">{laneLabel(job)}</td>
                                <td className="px-3 py-2 align-top">
                                  <p className="line-clamp-2 text-sm text-zinc-200">{job.askSummary || job.summary || job.detail || 'No request summary'}</p>
                                  <p className="mt-1 truncate text-[11px] text-zinc-600">{job.detail || '—'}</p>
                                </td>
                                <td className="px-3 py-2 align-top text-[12px] text-zinc-300">{requesterLabel(job)}</td>
                                <td className="px-3 py-2 align-top">
                                  <p className="truncate text-[12px] text-zinc-300">{targetLabel(job)}</p>
                                  <p className="mt-1 truncate text-[11px] text-zinc-600">{job.channel} · {job.chatId || '—'}</p>
                                </td>
                                <td className="px-3 py-2 align-top text-[11px] text-zinc-500">{compactPath(job.workspace)}</td>
                                <td className="px-3 py-2 align-top">
                                  <p className="text-[12px] text-zinc-300">{formatRelative(job.updatedAt)}</p>
                                  <p className="mt-1 text-[11px] text-zinc-600">{formatDuration(job.durationSec)}</p>
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </div>
            </Card>

            <Card title="Selected Job">
              {selectedJob ? (
                <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <StatusBadge variant={stateVariant(selectedJob.state)} dot>{selectedJob.state}</StatusBadge>
                        <StatusBadge variant={selectedJob.channel === 'discord' ? 'info' : 'ready'}>{selectedJob.channel}</StatusBadge>
                        <StatusBadge variant={selectedJob.lane === 'btw' ? 'warning' : 'muted'}>{laneLabel(selectedJob)}</StatusBadge>
                        {selectedJob.stale && <StatusBadge variant="warning">stale</StatusBadge>}
                      </div>
                      <h3 className="text-lg font-semibold text-zinc-100">{selectedJob.askSummary || 'No request summary captured'}</h3>
                      <p className="text-sm text-zinc-500">{selectedJob.detail || selectedJob.summary || 'No detail captured.'}</p>
                    </div>

                    {selectedJob.lastError && (
                      <div className="rounded-lg border border-red-500/20 bg-red-500/8 px-4 py-3">
                        <div className="flex items-center gap-2 text-red-300">
                          <AlertTriangle className="h-4 w-4" />
                          <span className="text-sm font-medium">Last error</span>
                        </div>
                        <pre className="mt-2 whitespace-pre-wrap break-words text-xs leading-relaxed text-red-200/90 font-mono">{selectedJob.lastError}</pre>
                      </div>
                    )}

                    <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface-50/15">
                      <table className="w-full table-fixed">
                        <tbody className="divide-y divide-border-subtle text-sm">
                          {[
                            ['Job ID', selectedJob.id],
                            ['Short ID', selectedJob.shortId || '—'],
                            ['Requester', requesterLabel(selectedJob)],
                            ['User ID', selectedJob.userId || '—'],
                            ['Room', `${selectedJob.channel} · ${selectedJob.chatId || '—'}`],
                            ['Route label', selectedJob.routeLabel || '—'],
                            ['Workspace', selectedJob.workspace || '—'],
                            ['Session key', selectedJob.sessionKey || '—'],
                            ['Status card', selectedJob.statusMessageId || '—'],
                            ['Updated', selectedJob.updatedAt ? new Date(selectedJob.updatedAt).toLocaleString() : '—'],
                          ].map(([label, value]) => (
                            <tr key={label}>
                              <td className="w-32 px-3 py-2 align-top text-xs uppercase tracking-[0.15em] text-zinc-500">{label}</td>
                              <td className="px-3 py-2 align-top text-sm text-zinc-300 break-all">{value}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>

                  <div className="space-y-3">
                    <div className="rounded-lg border border-border-subtle bg-surface-50/15 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">Queue health</p>
                      <div className="mt-3 grid grid-cols-2 gap-3 text-sm">
                        <div>
                          <p className="text-zinc-500">Phase</p>
                          <p className="mt-1 text-zinc-200">{selectedJob.phase || '—'}</p>
                        </div>
                        <div>
                          <p className="text-zinc-500">Runtime</p>
                          <p className="mt-1 text-zinc-200">{selectedJob.runtimeKey || 'cloud'}</p>
                        </div>
                        <div>
                          <p className="text-zinc-500">Started</p>
                          <p className="mt-1 text-zinc-200">{formatRelative(selectedJob.startedAt)}</p>
                        </div>
                        <div>
                          <p className="text-zinc-500">Elapsed</p>
                          <p className="mt-1 text-zinc-200">{formatDuration(selectedJob.durationSec)}</p>
                        </div>
                      </div>
                    </div>

                    <div className="rounded-lg border border-border-subtle bg-surface-50/15 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">Discord control</p>
                      <p className="mt-2 text-sm leading-6 text-zinc-400">
                        Use the Discord job card for this request if you need to intervene. The short ID
                        <span className="mx-1 rounded bg-surface-200 px-1.5 py-0.5 font-mono text-zinc-200">{selectedJob.shortId || '—'}</span>
                        is the one users see in Discord for actions like status, cancel, or force. This page stays read-only so it cannot drift from the gateway's real queue state.
                      </p>
                    </div>
                  </div>
                </div>
              ) : (
                <EmptyState icon={Users} title="No job selected" description="Pick a ledger row to inspect its state, request, and error details." />
              )}
            </Card>
          </div>
        </section>
      </main>
    </>
  );
}
