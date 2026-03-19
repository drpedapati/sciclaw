import { useState, useEffect } from 'react';
import { Clock, Plus, Trash2, ToggleLeft, ToggleRight, RefreshCw, Loader2 } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import { getCronJobs, addCronJob, toggleCronJob, removeCronJob, type CronJob } from '../lib/api';

export default function SchedulePage() {
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [addMode, setAddMode] = useState(false);
  const [description, setDescription] = useState('');
  const [adding, setAdding] = useState(false);
  const [deleteJob, setDeleteJob] = useState<CronJob | null>(null);
  const [flash, setFlash] = useState('');
  const [loaded, setLoaded] = useState(false);

  const fetchData = async () => {
    try {
      const data = await getCronJobs();
      setJobs(data);
      setLoaded(true);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleAdd = async () => {
    if (!description.trim()) return;
    setAdding(true);
    try {
      await addCronJob(description.trim());
      showFlash('Task scheduled');
      setAddMode(false);
      setDescription('');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setAdding(false);
    }
  };

  const handleToggle = async (id: string) => {
    try {
      await toggleCronJob(id);
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
  };

  const handleRemove = async () => {
    if (!deleteJob) return;
    try {
      await removeCronJob(deleteJob.id);
      showFlash('Task removed');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setDeleteJob(null);
  };

  return (
    <>
      <TopBar title="Schedule" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <Card
          title={`Scheduled Tasks (${jobs.length})`}
          actions={
            <div className="flex gap-2">
              <button onClick={fetchData} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
                <RefreshCw className="w-3.5 h-3.5" />
              </button>
              <button
                onClick={() => setAddMode(true)}
                className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
              >
                <Plus className="w-3 h-3" />
                Add Task
              </button>
            </div>
          }
        >
          {addMode && (
            <div className="mb-4 p-3 rounded-md border border-brand/20 bg-brand/5 animate-fade-in">
              <label className="text-xs text-zinc-500 block mb-1">Describe your task in natural language</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder='e.g. "Check PubMed for ALS papers every morning at 9am"'
                  className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleAdd();
                    if (e.key === 'Escape') { setAddMode(false); setDescription(''); }
                  }}
                />
                <button
                  onClick={handleAdd}
                  disabled={adding || !description.trim()}
                  className="px-4 py-2 text-sm rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                >
                  {adding ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Schedule'}
                </button>
              </div>
            </div>
          )}

          {!loaded ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-14 rounded-md bg-surface-50/30 animate-pulse" />
              ))}
            </div>
          ) : jobs.length === 0 ? (
            <EmptyState
              icon={Clock}
              title="No scheduled tasks"
              description="Create scheduled tasks using natural language descriptions."
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-border">
                    <th className="text-left px-4 py-2 text-xs uppercase text-zinc-500 font-medium tracking-wider">Name</th>
                    <th className="text-left px-4 py-2 text-xs uppercase text-zinc-500 font-medium tracking-wider">Schedule</th>
                    <th className="text-left px-4 py-2 text-xs uppercase text-zinc-500 font-medium tracking-wider">Status</th>
                    <th className="text-left px-4 py-2 text-xs uppercase text-zinc-500 font-medium tracking-wider">Next Run</th>
                    <th className="w-20"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {jobs.map((job) => (
                    <tr key={job.id} className="group hover:bg-surface-50/30 transition-colors">
                      <td className="px-4 py-3 text-sm text-zinc-200">{job.name}</td>
                      <td className="px-4 py-3 text-xs text-zinc-400 font-mono">{job.schedule}</td>
                      <td className="px-4 py-3">
                        <StatusBadge variant={job.enabled ? 'ready' : 'muted'} dot>
                          {job.enabled ? 'Enabled' : 'Disabled'}
                        </StatusBadge>
                      </td>
                      <td className="px-4 py-3 text-xs text-zinc-500 font-mono">{job.nextRun || '—'}</td>
                      <td className="px-4 py-3">
                        <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          <button
                            onClick={() => handleToggle(job.id)}
                            className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors"
                            title={job.enabled ? 'Disable' : 'Enable'}
                          >
                            {job.enabled ? <ToggleRight className="w-4 h-4 text-brand" /> : <ToggleLeft className="w-4 h-4" />}
                          </button>
                          <button
                            onClick={() => setDeleteJob(job)}
                            className="p-1 rounded hover:bg-red-500/10 text-zinc-600 hover:text-red-400 transition-colors"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </main>

      <ConfirmDialog
        open={!!deleteJob}
        title="Remove Task"
        message={`Remove "${deleteJob?.name}"?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemove}
        onCancel={() => setDeleteJob(null)}
      />
    </>
  );
}
