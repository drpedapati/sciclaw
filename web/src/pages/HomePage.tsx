import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  CheckCircle2, XCircle, AlertTriangle, Server, Radio,
  Shield, Settings as SettingsIcon, Cpu, Zap, ArrowRight,
  Loader2,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import { useSnapshot } from '../hooks/useSnapshot';
import { getChecklist, runSmokeTest, type SetupChecklist } from '../lib/api';

function CheckItem({ label, ok, nav }: { label: string; ok: boolean; nav: string }) {
  const navigate = useNavigate();
  return (
    <button
      onClick={() => navigate(nav)}
      className="flex items-center gap-3 w-full text-left px-3 py-2 rounded-md hover:bg-surface-50/30 transition-colors duration-150 group"
    >
      {ok ? (
        <CheckCircle2 className="w-4 h-4 text-brand flex-shrink-0" />
      ) : (
        <XCircle className="w-4 h-4 text-zinc-600 flex-shrink-0" />
      )}
      <span className={`text-sm ${ok ? 'text-zinc-400' : 'text-zinc-200'}`}>{label}</span>
      <ArrowRight className="w-3 h-3 text-zinc-600 ml-auto opacity-0 group-hover:opacity-100 transition-opacity" />
    </button>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  variant = 'muted',
  onClick,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  variant?: 'ready' | 'error' | 'warning' | 'muted';
  onClick?: () => void;
}) {
  const colorMap = {
    ready: 'text-brand',
    error: 'text-red-400',
    warning: 'text-amber-400',
    muted: 'text-zinc-400',
  };
  return (
    <div
      onClick={onClick}
      className={`rounded-lg border border-border bg-surface-100 p-4 ${onClick ? 'cursor-pointer hover:bg-surface-50/30 transition-colors duration-150' : ''}`}
    >
      <div className="flex items-center gap-2 mb-2">
        <Icon className={`w-4 h-4 ${colorMap[variant]}`} />
        <span className="text-xs text-zinc-500">{label}</span>
      </div>
      <p className={`text-lg font-semibold ${colorMap[variant]}`}>{value}</p>
    </div>
  );
}

export default function HomePage() {
  const { snapshot } = useSnapshot();
  const navigate = useNavigate();
  const [checklist, setChecklist] = useState<SetupChecklist | null>(null);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; output: string } | null>(null);

  useEffect(() => {
    getChecklist().then(setChecklist).catch(() => {});
  }, []);

  const handleSmokeTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const result = await runSmokeTest();
      setTestResult(result);
    } catch (e) {
      setTestResult({ ok: false, output: String(e) });
    } finally {
      setTesting(false);
    }
  };

  const gatewayVariant = snapshot.ServiceRunning ? 'ready' : snapshot.ServiceInstalled ? 'warning' : 'error';
  const discordVariant = snapshot.Discord.Status === 'ready' ? 'ready' : snapshot.Discord.Enabled ? 'warning' : 'muted';
  const telegramVariant = snapshot.Telegram.Status === 'ready' ? 'ready' : snapshot.Telegram.Enabled ? 'warning' : 'muted';

  return (
    <>
      <TopBar title="Home" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {/* Status cards */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <StatCard
            icon={Server}
            label="Gateway"
            value={snapshot.ServiceRunning ? 'Running' : snapshot.ServiceInstalled ? 'Stopped' : 'Not Installed'}
            variant={gatewayVariant}
            onClick={() => navigate('/gateway')}
          />
          <StatCard
            icon={Radio}
            label="Discord"
            value={snapshot.Discord.Status === 'ready' ? 'Connected' : snapshot.Discord.Enabled ? 'Enabled' : 'Off'}
            variant={discordVariant}
            onClick={() => navigate('/channels')}
          />
          <StatCard
            icon={Radio}
            label="Telegram"
            value={snapshot.Telegram.Status === 'ready' ? 'Connected' : snapshot.Telegram.Enabled ? 'Enabled' : 'Off'}
            variant={telegramVariant}
            onClick={() => navigate('/channels')}
          />
          <StatCard
            icon={Cpu}
            label="Model"
            value={snapshot.ActiveModel || 'Not set'}
            variant={snapshot.ActiveModel ? 'ready' : 'muted'}
            onClick={() => navigate('/models')}
          />
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          {/* Setup checklist */}
          <Card title="Setup Checklist">
            {checklist ? (
              <div className="space-y-0.5">
                <CheckItem label="Configuration initialized" ok={checklist.config} nav="/settings" />
                <CheckItem label="Authentication configured" ok={checklist.auth} nav="/login" />
                <CheckItem label="Channel connected" ok={checklist.channel} nav="/channels" />
                <CheckItem label="Gateway service installed" ok={checklist.service} nav="/gateway" />
              </div>
            ) : (
              <div className="space-y-2">
                {[1, 2, 3, 4].map((i) => (
                  <div key={i} className="h-9 rounded-md bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            )}
          </Card>

          {/* System info */}
          <Card title="System">
            <div className="space-y-3 text-sm">
              {[
                ['State', snapshot.State],
                ['IP', snapshot.IPv4 || 'localhost'],
                ['Load', snapshot.Load || '—'],
                ['Memory', snapshot.Memory || '—'],
                ['Workspace', snapshot.WorkspacePath || '—'],
              ].map(([label, value]) => (
                <div key={label} className="flex items-center justify-between">
                  <span className="text-zinc-500">{label}</span>
                  <span className="text-zinc-300 font-mono text-xs">{value}</span>
                </div>
              ))}
            </div>
          </Card>
        </div>

        {/* Provider status */}
        <Card title="Providers">
          <div className="grid grid-cols-2 gap-4">
            {[
              { name: 'OpenAI', status: snapshot.OpenAI },
              { name: 'Anthropic', status: snapshot.Anthropic },
            ].map(({ name, status }) => (
              <div
                key={name}
                className="flex items-center justify-between p-3 rounded-md border border-border-subtle cursor-pointer hover:bg-surface-50/30 transition-colors duration-150"
                onClick={() => navigate('/login')}
              >
                <div className="flex items-center gap-2">
                  <Shield className="w-4 h-4 text-zinc-500" />
                  <span className="text-sm text-zinc-300">{name}</span>
                </div>
                <StatusBadge
                  variant={status === 'ready' ? 'ready' : 'muted'}
                  dot
                >
                  {status === 'ready' ? 'Active' : 'Not Set'}
                </StatusBadge>
              </div>
            ))}
          </div>
        </Card>

        {/* Connection test */}
        <Card
          title="Connection Test"
          actions={
            <button
              onClick={handleSmokeTest}
              disabled={testing}
              className="flex items-center gap-2 px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors duration-150 font-medium"
            >
              {testing ? (
                <Loader2 className="w-3 h-3 animate-spin" />
              ) : (
                <Zap className="w-3 h-3" />
              )}
              {testing ? 'Testing...' : 'Run Test'}
            </button>
          }
        >
          {testResult ? (
            <div className={`flex items-start gap-3 p-3 rounded-md border ${
              testResult.ok
                ? 'border-brand/30 bg-brand/5'
                : 'border-red-500/30 bg-red-500/5'
            }`}>
              {testResult.ok ? (
                <CheckCircle2 className="w-4 h-4 text-brand mt-0.5 flex-shrink-0" />
              ) : (
                <AlertTriangle className="w-4 h-4 text-red-400 mt-0.5 flex-shrink-0" />
              )}
              <pre className="text-xs text-zinc-300 font-mono whitespace-pre-wrap flex-1">
                {testResult.output}
              </pre>
            </div>
          ) : (
            <p className="text-sm text-zinc-500">
              Send a quick message to verify your AI provider connection is working.
            </p>
          )}
        </Card>
      </main>
    </>
  );
}
