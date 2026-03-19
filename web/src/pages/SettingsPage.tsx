import { useState, useEffect } from 'react';
import { RefreshCw, AlertTriangle } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import { getSettings, updateSetting, type Settings } from '../lib/api';

interface SettingRow {
  section: string;
  key: string;
  label: string;
  path: string;
  type: 'toggle' | 'text' | 'password' | 'enum' | 'readonly';
  options?: string[];
  risky?: boolean;
  restartRequired?: boolean;
}

const settingRows: SettingRow[] = [
  { section: 'Channels', key: 'discord.enabled', label: 'Discord Enabled', path: 'discord.enabled', type: 'toggle', risky: true, restartRequired: true },
  { section: 'Channels', key: 'telegram.enabled', label: 'Telegram Enabled', path: 'telegram.enabled', type: 'toggle', risky: true, restartRequired: true },
  { section: 'Routing', key: 'routing.enabled', label: 'Routing Enabled', path: 'routing.enabled', type: 'toggle', restartRequired: true },
  { section: 'Routing', key: 'routing.unmappedBehavior', label: 'Unmapped Behavior', path: 'routing.unmappedBehavior', type: 'enum', options: ['block', 'mention_only', 'default'], restartRequired: true },
  { section: 'Agent', key: 'agent.defaultModel', label: 'Default Model', path: 'agent.defaultModel', type: 'text' },
  { section: 'Agent', key: 'agent.reasoningEffort', label: 'Reasoning Effort', path: 'agent.reasoningEffort', type: 'enum', options: ['none', 'minimal', 'low', 'medium', 'high', 'xhigh'] },
  { section: 'Integrations', key: 'integrations.pubmedApiKey', label: 'PubMed API Key', path: 'integrations.pubmedApiKey', type: 'password' },
  { section: 'Service', key: 'service.autoStart', label: 'Auto-Start on Boot', path: 'service.autoStart', type: 'toggle' },
  { section: 'Service', key: 'service.installed', label: 'Installed', path: 'service.installed', type: 'readonly' },
  { section: 'Service', key: 'service.running', label: 'Running', path: 'service.running', type: 'readonly' },
  { section: 'General', key: 'general.workspacePath', label: 'Workspace Path', path: 'general.workspacePath', type: 'readonly' },
];

function getNestedValue(obj: Record<string, unknown>, path: string): unknown {
  let current: unknown = obj;
  for (const key of path.split('.')) {
    if (!current || typeof current !== 'object') {
      return undefined;
    }
    current = (current as Record<string, unknown>)[key];
  }
  return current;
}

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [editField, setEditField] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [confirm, setConfirm] = useState<SettingRow | null>(null);
  const [flash, setFlash] = useState('');
  const [changedPaths, setChangedPaths] = useState<Set<string>>(new Set());

  const fetchData = async () => {
    try {
      const data = await getSettings();
      setSettings(data);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleSave = async (row: SettingRow, value: unknown) => {
    try {
      await updateSetting(row.path, value);
      showFlash('Setting updated');
      if (row.restartRequired) {
        setChangedPaths((prev) => new Set(prev).add(row.path));
      }
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setEditField(null);
    setConfirm(null);
  };

  const handleToggle = (row: SettingRow) => {
    const current = getNestedValue(settings as unknown as Record<string, unknown>, row.path);
    if (row.risky) {
      setConfirm(row);
    } else {
      handleSave(row, !current);
    }
  };

  const renderValue = (row: SettingRow) => {
    if (!settings) return '—';
    const val = getNestedValue(settings as unknown as Record<string, unknown>, row.path);

    if (row.type === 'readonly') {
      if (typeof val === 'boolean') {
        return val ? (
          <span className="text-brand text-sm">Yes</span>
        ) : (
          <span className="text-zinc-500 text-sm">No</span>
        );
      }
      return <span className="text-zinc-300 text-sm font-mono">{String(val || '—')}</span>;
    }

    if (row.type === 'toggle') {
      return (
        <button
          onClick={() => handleToggle(row)}
          className={`relative w-9 h-5 rounded-full transition-colors duration-200 ${
            val ? 'bg-brand' : 'bg-zinc-700'
          }`}
        >
          <span
            className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform duration-200 ${
              val ? 'translate-x-4' : 'translate-x-0.5'
            }`}
          />
        </button>
      );
    }

    if (editField === row.key) {
      if (row.type === 'enum') {
        return (
          <div className="flex gap-1">
            {row.options?.map((opt) => (
              <button
                key={opt}
                onClick={() => handleSave(row, opt)}
                className={`px-2 py-1 text-xs rounded-md transition-colors ${
                  val === opt ? 'bg-brand text-surface-500' : 'border border-border text-zinc-400 hover:bg-surface-50'
                }`}
              >
                {opt}
              </button>
            ))}
          </div>
        );
      }
      return (
        <input
          type={row.type === 'password' ? 'password' : 'text'}
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          className="w-64 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 font-mono focus:outline-none focus:ring-1 focus:ring-brand/50"
          autoFocus
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleSave(row, editValue);
            if (e.key === 'Escape') setEditField(null);
          }}
          onBlur={() => setEditField(null)}
        />
      );
    }

    const display = row.type === 'password'
      ? (val ? '••••••••' : 'Not set')
      : String(val || '—');

    return (
      <button
        onClick={() => {
          if (row.type === 'enum') {
            setEditField(row.key);
          } else {
            setEditField(row.key);
            setEditValue(row.type === 'password' ? '' : String(val || ''));
          }
        }}
        className="text-sm text-zinc-300 font-mono hover:text-zinc-100 transition-colors text-right"
      >
        {display}
      </button>
    );
  };

  // Group by section
  const sections = settingRows.reduce((acc, row) => {
    (acc[row.section] = acc[row.section] || []).push(row);
    return acc;
  }, {} as Record<string, SettingRow[]>);

  return (
    <>
      <TopBar title="Settings" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        {changedPaths.size > 0 && (
          <div className="flex items-center gap-2 rounded-md bg-amber-500/10 border border-amber-500/20 px-4 py-2 text-sm text-amber-400">
            <AlertTriangle className="w-4 h-4 flex-shrink-0" />
            Service restart required for changes to take effect.
          </div>
        )}

        {Object.entries(sections).map(([section, rows]) => (
          <Card
            key={section}
            title={section}
            actions={
              section === Object.keys(sections)[0] ? (
                <button onClick={fetchData} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
                  <RefreshCw className="w-3.5 h-3.5" />
                </button>
              ) : undefined
            }
          >
            {!settings ? (
              <div className="space-y-3">
                {rows.map((_, i) => (
                  <div key={i} className="h-8 rounded bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : (
              <div className="divide-y divide-border-subtle">
                {rows.map((row) => (
                  <div key={row.key} className="flex items-center justify-between py-3">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-zinc-500">{row.label}</span>
                      {changedPaths.has(row.path) && (
                        <StatusBadge variant="warning">restart</StatusBadge>
                      )}
                    </div>
                    {renderValue(row)}
                  </div>
                ))}
              </div>
            )}
          </Card>
        ))}
      </main>

      <ConfirmDialog
        open={!!confirm}
        title="Confirm Change"
        message={`Are you sure you want to toggle ${confirm?.label}? This may disrupt active connections.`}
        confirmLabel="Confirm"
        danger
        onConfirm={() => {
          if (confirm && settings) {
            const current = getNestedValue(settings as unknown as Record<string, unknown>, confirm.path);
            handleSave(confirm, !current);
          }
        }}
        onCancel={() => setConfirm(null)}
      />
    </>
  );
}
