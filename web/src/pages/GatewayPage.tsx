import { useState, useEffect } from 'react';
import {
  Play, Square, RotateCcw, Download, Trash2,
  FileText, Loader2, CheckCircle2, XCircle, RefreshCw, Trophy,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import { useSnapshot } from '../hooks/useSnapshot';
import { useFlash, FlashBanner } from '../hooks/useFlash';
import { serviceAction, getServiceLogs } from '../lib/api';

export default function GatewayPage() {
  const { snapshot, refresh } = useSnapshot();
  const [logs, setLogs] = useState<string | null>(null);
  const [actionRunning, setActionRunning] = useState<string | null>(null);
  const [lastAction, setLastAction] = useState<{ name: string; ok: boolean; output: string } | null>(null);
  const { flash, showFlash } = useFlash();
  const [successStreak, setSuccessStreak] = useState(0);

  const handleAction = async (action: string) => {
    setActionRunning(action);
    try {
      const result = await serviceAction(action);
      setLastAction({ name: action, ok: result.ok, output: result.output });
      if (result.ok) {
        setSuccessStreak((s) => s + 1);
        showFlash(`${action} completed`);
      } else {
        setSuccessStreak(0);
        showFlash(`${action} failed`);
      }
      refresh();
    } catch (e) {
      setLastAction({ name: action, ok: false, output: String(e) });
      setSuccessStreak(0);
      showFlash(`Error: ${e}`);
    } finally {
      setActionRunning(null);
    }
  };

  const fetchLogs = async () => {
    try {
      const data = await getServiceLogs();
      setLogs(data.logs);
    } catch (e) {
      setLogs(`Error fetching logs: ${e}`);
    }
  };

  useEffect(() => { fetchLogs(); }, []);

  const ActionButton = ({
    icon: Icon,
    label,
    action,
    variant = 'default',
    disabled,
  }: {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    action: string;
    variant?: 'default' | 'primary' | 'danger';
    disabled?: boolean;
  }) => {
    const isRunning = actionRunning === action;
    const styles = {
      default: 'border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50',
      primary: 'bg-brand text-surface-500 hover:bg-brand-500',
      danger: 'border border-red-500/30 text-red-400 hover:bg-red-500/10',
    };
    return (
      <button
        onClick={() => handleAction(action)}
        disabled={isRunning || disabled}
        className={`flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md disabled:opacity-50 transition-colors duration-150 font-medium ${styles[variant]}`}
      >
        {isRunning ? <Loader2 className="w-3 h-3 animate-spin" /> : <Icon className="w-3 h-3" />}
        {label}
      </button>
    );
  };

  return (
    <>
      <TopBar title="Gateway" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        <FlashBanner message={flash} />

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          {/* Service Status */}
          <Card title="Service">
            <div className="space-y-4">
              <div className="space-y-3">
                {[
                  ['Installed', snapshot.ServiceInstalled],
                  ['Running', snapshot.ServiceRunning],
                  ['Auto-start', snapshot.ServiceAutoStart],
                ].map(([label, value]) => (
                  <div key={label as string} className="flex items-center justify-between">
                    <span className="text-sm text-zinc-500">{label as string}</span>
                    {value ? (
                      <span className="flex items-center gap-1.5 text-sm text-brand">
                        <CheckCircle2 className="w-3.5 h-3.5" /> Yes
                      </span>
                    ) : (
                      <span className="flex items-center gap-1.5 text-sm text-zinc-500">
                        <XCircle className="w-3.5 h-3.5" /> No
                      </span>
                    )}
                  </div>
                ))}
              </div>

              {/* Last action */}
              {lastAction && (
                <div className={`flex items-start gap-2 p-2.5 rounded-md text-xs ${
                  lastAction.ok
                    ? 'bg-brand/5 border border-brand/20 text-brand'
                    : 'bg-red-500/5 border border-red-500/20 text-red-400'
                }`}>
                  {lastAction.ok ? <CheckCircle2 className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" /> : <XCircle className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />}
                  <span>{lastAction.name}: {lastAction.ok ? 'OK' : lastAction.output}</span>
                </div>
              )}

              {/* Success streak */}
              {successStreak > 1 && (
                <div className="flex items-center gap-1.5 text-xs text-amber-400">
                  <Trophy className="w-3 h-3" />
                  {successStreak} consecutive successes
                </div>
              )}

              {/* Actions */}
              <div className="flex flex-wrap gap-2 pt-2 border-t border-border-subtle">
                {snapshot.ServiceRunning ? (
                  <>
                    <ActionButton icon={Square} label="Stop" action="stop" variant="danger" />
                    <ActionButton icon={RotateCcw} label="Restart" action="restart" variant="primary" />
                  </>
                ) : snapshot.ServiceInstalled ? (
                  <>
                    <ActionButton icon={Play} label="Start" action="start" variant="primary" />
                    <ActionButton icon={Download} label="Reinstall" action="install" />
                    <ActionButton icon={Trash2} label="Uninstall" action="uninstall" variant="danger" />
                  </>
                ) : (
                  <ActionButton icon={Download} label="Install" action="install" variant="primary" />
                )}
                <ActionButton icon={RefreshCw} label="Refresh" action="refresh" />
              </div>
            </div>
          </Card>

          {/* Logs */}
          <Card
            title="Logs"
            actions={
              <button
                onClick={fetchLogs}
                className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors"
              >
                <FileText className="w-3.5 h-3.5" />
              </button>
            }
          >
            <div className="bg-surface-300 rounded-md border border-border-subtle p-3 max-h-80 overflow-y-auto">
              <pre className="text-xs text-zinc-400 font-mono whitespace-pre-wrap leading-relaxed">
                {logs ?? 'No logs loaded. Click refresh to fetch.'}
              </pre>
            </div>
          </Card>
        </div>
      </main>
    </>
  );
}
