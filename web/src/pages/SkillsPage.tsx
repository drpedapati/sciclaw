import { useState, useEffect } from 'react';
import { Puzzle, Plus, Trash2, Loader2, RefreshCw } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import { getSkills, installSkill, removeSkill, type Skill } from '../lib/api';

function sourceBadge(source: string) {
  switch (source) {
    case 'workspace': return <StatusBadge variant="ready">workspace</StatusBadge>;
    case 'builtin': return <StatusBadge variant="info">builtin</StatusBadge>;
    case 'global': return <StatusBadge variant="warning">global</StatusBadge>;
    default: return <StatusBadge variant="muted">{source}</StatusBadge>;
  }
}

export default function SkillsPage() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selected, setSelected] = useState(0);
  const [installMode, setInstallMode] = useState(false);
  const [installPath, setInstallPath] = useState('');
  const [installing, setInstalling] = useState(false);
  const [deleteSkill, setDeleteSkill] = useState<Skill | null>(null);
  const [flash, setFlash] = useState('');
  const [loaded, setLoaded] = useState(false);

  const fetchData = async () => {
    try {
      const data = await getSkills();
      setSkills(data);
      setLoaded(true);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleInstall = async () => {
    if (!installPath.trim()) return;
    setInstalling(true);
    try {
      await installSkill(installPath.trim());
      showFlash('Skill installed');
      setInstallMode(false);
      setInstallPath('');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setInstalling(false);
    }
  };

  const handleRemove = async () => {
    if (!deleteSkill) return;
    try {
      await removeSkill(deleteSkill.name);
      showFlash('Skill removed');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setDeleteSkill(null);
  };

  const selectedSkill = skills[selected];

  return (
    <>
      <TopBar title="Skills" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
          {/* Skills list */}
          <div className="lg:col-span-2">
            <Card
              title={`Skills (${skills.length})`}
              actions={
                <div className="flex gap-2">
                  <button onClick={fetchData} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
                    <RefreshCw className="w-3.5 h-3.5" />
                  </button>
                  <button
                    onClick={() => setInstallMode(true)}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
                  >
                    <Plus className="w-3 h-3" />
                    Install
                  </button>
                </div>
              }
            >
              {installMode && (
                <div className="mb-4 p-3 rounded-md border border-brand/20 bg-brand/5 animate-fade-in">
                  <label className="text-xs text-zinc-500 block mb-1">Skill Path</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={installPath}
                      onChange={(e) => setInstallPath(e.target.value)}
                      placeholder="e.g. drpedapati/sciclaw-skills/weather"
                      className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 font-mono placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                      autoFocus
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') handleInstall();
                        if (e.key === 'Escape') { setInstallMode(false); setInstallPath(''); }
                      }}
                    />
                    <button
                      onClick={handleInstall}
                      disabled={installing || !installPath.trim()}
                      className="px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
                    >
                      {installing ? <Loader2 className="w-3 h-3 animate-spin" /> : 'Install'}
                    </button>
                  </div>
                </div>
              )}

              {!loaded ? (
                <div className="space-y-2">
                  {[1, 2, 3, 4].map((i) => (
                    <div key={i} className="h-10 rounded-md bg-surface-50/30 animate-pulse" />
                  ))}
                </div>
              ) : skills.length === 0 ? (
                <EmptyState
                  icon={Puzzle}
                  title="No skills installed"
                  description="Install skills to extend sciClaw's capabilities."
                />
              ) : (
                <div className="divide-y divide-border-subtle max-h-[500px] overflow-y-auto">
                  {skills.map((skill, idx) => (
                    <button
                      key={skill.name}
                      onClick={() => setSelected(idx)}
                      className={`flex items-center justify-between w-full text-left px-3 py-2.5 transition-colors group ${
                        selected === idx ? 'bg-brand/5' : 'hover:bg-surface-50/30'
                      }`}
                    >
                      <div className="flex items-center gap-3 min-w-0">
                        <Puzzle className={`w-4 h-4 flex-shrink-0 ${selected === idx ? 'text-brand' : 'text-zinc-600'}`} />
                        <span className="text-sm text-zinc-200 truncate">{skill.name}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        {sourceBadge(skill.source)}
                        <button
                          onClick={(e) => { e.stopPropagation(); setDeleteSkill(skill); }}
                          className="p-1 rounded hover:bg-red-500/10 text-zinc-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-all"
                        >
                          <Trash2 className="w-3 h-3" />
                        </button>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </Card>
          </div>

          {/* Detail pane */}
          <div className="lg:col-span-1">
            <Card title="Details">
              {selectedSkill ? (
                <div className="space-y-3">
                  <div>
                    <p className="text-xs text-zinc-500 mb-1">Name</p>
                    <p className="text-sm text-zinc-200 font-mono">{selectedSkill.name}</p>
                  </div>
                  <div>
                    <p className="text-xs text-zinc-500 mb-1">Source</p>
                    {sourceBadge(selectedSkill.source)}
                  </div>
                  <div>
                    <p className="text-xs text-zinc-500 mb-1">Description</p>
                    <p className="text-sm text-zinc-400 leading-relaxed">
                      {selectedSkill.description || 'No description available.'}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-zinc-500 mb-1">Status</p>
                    <StatusBadge variant={selectedSkill.status === 'active' ? 'ready' : 'muted'}>
                      {selectedSkill.status || 'unknown'}
                    </StatusBadge>
                  </div>
                </div>
              ) : (
                <p className="text-sm text-zinc-500">Select a skill to view details.</p>
              )}
            </Card>
          </div>
        </div>
      </main>

      <ConfirmDialog
        open={!!deleteSkill}
        title="Remove Skill"
        message={`Remove "${deleteSkill?.name}"?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemove}
        onCancel={() => setDeleteSkill(null)}
      />
    </>
  );
}
