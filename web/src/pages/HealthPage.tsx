import { useState } from 'react';
import {
  Stethoscope, CheckCircle2, AlertTriangle, XCircle, MinusCircle,
  Loader2, RefreshCw,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import { runDoctor, type DoctorReport } from '../lib/api';

function statusIcon(status: string) {
  switch (status) {
    case 'pass': return <CheckCircle2 className="w-4 h-4 text-brand flex-shrink-0" />;
    case 'warn': return <AlertTriangle className="w-4 h-4 text-amber-400 flex-shrink-0" />;
    case 'error': return <XCircle className="w-4 h-4 text-red-400 flex-shrink-0" />;
    default: return <MinusCircle className="w-4 h-4 text-zinc-600 flex-shrink-0" />;
  }
}

function statusBg(status: string) {
  switch (status) {
    case 'pass': return 'border-brand/20 bg-brand/5';
    case 'warn': return 'border-amber-500/20 bg-amber-500/5';
    case 'error': return 'border-red-500/20 bg-red-500/5';
    default: return 'border-border bg-surface-50/30';
  }
}

export default function HealthPage() {
  const [report, setReport] = useState<DoctorReport | null>(null);
  const [running, setRunning] = useState(false);

  const handleRun = async () => {
    setRunning(true);
    try {
      const data = await runDoctor();
      setReport(data);
    } catch (e) {
      setReport(null);
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <TopBar title="Health" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        <Card
          title="Health Check"
          actions={
            <button
              onClick={handleRun}
              disabled={running}
              className="flex items-center gap-2 px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
            >
              {running ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
              {running ? 'Running...' : 'Run Check'}
            </button>
          }
        >
          {!report && !running ? (
            <div className="flex flex-col items-center justify-center py-12 text-center">
              <Stethoscope className="w-10 h-10 text-zinc-700 mb-3" />
              <p className="text-sm font-medium text-zinc-400">Run a health check</p>
              <p className="text-xs text-zinc-600 mt-1">
                Diagnose your sciClaw installation, configuration, and connectivity.
              </p>
            </div>
          ) : running ? (
            <div className="flex items-center justify-center py-12 gap-3">
              <Loader2 className="w-5 h-5 text-brand animate-spin" />
              <span className="text-sm text-zinc-400">Running health checks...</span>
            </div>
          ) : report ? (
            <div className="space-y-4">
              {/* Summary */}
              <div className="flex items-center gap-4 p-3 rounded-md bg-surface-50/30 border border-border-subtle">
                <div className="flex items-center gap-4 text-xs">
                  <span className="flex items-center gap-1.5 text-brand">
                    <CheckCircle2 className="w-3.5 h-3.5" /> {report.passed} passed
                  </span>
                  <span className="flex items-center gap-1.5 text-amber-400">
                    <AlertTriangle className="w-3.5 h-3.5" /> {report.warnings} warnings
                  </span>
                  <span className="flex items-center gap-1.5 text-red-400">
                    <XCircle className="w-3.5 h-3.5" /> {report.errors} errors
                  </span>
                  <span className="flex items-center gap-1.5 text-zinc-500">
                    <MinusCircle className="w-3.5 h-3.5" /> {report.skipped} skipped
                  </span>
                </div>
                <div className="ml-auto text-xs text-zinc-600 font-mono">
                  v{report.version} · {report.os}/{report.arch}
                </div>
              </div>

              {/* Checks - errors first, then warnings, then passed */}
              <div className="space-y-2">
                {[...report.checks]
                  .sort((a, b) => {
                    const order = { error: 0, warn: 1, pass: 2, skip: 3 };
                    return (order[a.status as keyof typeof order] ?? 4) - (order[b.status as keyof typeof order] ?? 4);
                  })
                  .map((check, i) => (
                    <div
                      key={i}
                      className={`flex items-start gap-3 px-3 py-2.5 rounded-md border ${statusBg(check.status)}`}
                    >
                      {statusIcon(check.status)}
                      <div className="flex-1 min-w-0">
                        <p className="text-sm text-zinc-200">{check.name}</p>
                        {check.message && (
                          <p className="text-xs text-zinc-500 mt-0.5">{check.message}</p>
                        )}
                      </div>
                    </div>
                  ))}
              </div>
            </div>
          ) : null}
        </Card>
      </main>
    </>
  );
}
