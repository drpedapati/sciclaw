import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, Loader2, Mail, RefreshCw, Save, Send, ShieldCheck, Undo2 } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import { getEmailConfig, updateEmailConfig, sendTestEmail, type EmailConfig } from '../lib/api';

type EmailDraft = {
  enabled: boolean;
  address: string;
  displayName: string;
  apiKey: string;
  baseUrl: string;
  allowFrom: string;
};

function buildDraft(config: EmailConfig): EmailDraft {
  return {
    enabled: config.enabled,
    address: config.address || '',
    displayName: config.displayName || 'sciClaw',
    apiKey: '',
    baseUrl: config.baseUrl || 'https://api.resend.com',
    allowFrom: config.allowFrom.join('\n'),
  };
}

function normalizeAllowFrom(raw: string): string[] {
  return raw
    .replace(/\r\n/g, '\n')
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function draftChanged(draft: EmailDraft, config: EmailConfig | null): boolean {
  if (!config) return false;
  if (draft.enabled !== config.enabled) return true;
  if (draft.address.trim() !== (config.address || '').trim()) return true;
  if (draft.displayName.trim() !== (config.displayName || '').trim()) return true;
  if ((draft.baseUrl.trim() || 'https://api.resend.com') !== (config.baseUrl || 'https://api.resend.com')) return true;
  if (draft.apiKey.trim() !== '') return true;
  const nextAllow = normalizeAllowFrom(draft.allowFrom);
  if (nextAllow.length !== config.allowFrom.length) return true;
  return nextAllow.some((entry, index) => entry !== config.allowFrom[index]);
}

export default function EmailPage() {
  const [config, setConfig] = useState<EmailConfig | null>(null);
  const [draft, setDraft] = useState<EmailDraft | null>(null);
  const [testTo, setTestTo] = useState('');
  const [flash, setFlash] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const fetchData = async () => {
    setLoading(true);
    try {
      const data = await getEmailConfig();
      setConfig(data);
      setDraft(buildDraft(data));
      setError('');
      setTestTo((current) => current || data.address || '');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load email settings');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const dirty = useMemo(() => draftChanged(draft ?? {
    enabled: false,
    address: '',
    displayName: '',
    apiKey: '',
    baseUrl: '',
    allowFrom: '',
  }, config), [draft, config]);

  const previewAllowFrom = useMemo(() => normalizeAllowFrom(draft?.allowFrom || ''), [draft?.allowFrom]);
  const hasSender = !!draft?.address.trim();
  const hasApiKey = !!draft?.apiKey.trim() || !!config?.hasApiKey;
  const deliveryReady = !!draft?.enabled && hasSender && hasApiKey;

  const updateDraft = <K extends keyof EmailDraft>(key: K, value: EmailDraft[K]) => {
    setDraft((current) => current ? { ...current, [key]: value } : current);
  };

  const resetDraft = () => {
    if (!config) return;
    setDraft(buildDraft(config));
    showFlash('Draft reset');
  };

  const handleSave = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const payload: Partial<EmailConfig & { apiKey: string }> = {
        enabled: draft.enabled,
        address: draft.address.trim(),
        displayName: draft.displayName.trim() || 'sciClaw',
        baseUrl: draft.baseUrl.trim() || 'https://api.resend.com',
        allowFrom: normalizeAllowFrom(draft.allowFrom),
      };
      if (draft.apiKey.trim()) payload.apiKey = draft.apiKey.trim();
      await updateEmailConfig(payload);
      showFlash('Email settings saved');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    if (!testTo.trim()) return;
    setTesting(true);
    try {
      const result = await sendTestEmail(testTo.trim());
      showFlash(result.ok ? `Test email sent to ${testTo.trim()}` : result.output);
    } catch (e) {
      showFlash(`Error: ${e instanceof Error ? e.message : e}`);
    } finally {
      setTesting(false);
    }
  };

  return (
    <>
      <TopBar title="Email" />
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
              <p className="text-[11px] font-semibold uppercase tracking-[0.24em] text-brand/80">Outbound Email</p>
              <h2 className="text-xl font-semibold text-zinc-100">Managed Resend delivery, sender identity, and send-test workflow</h2>
              <p className="text-sm text-zinc-500">
                Email is send-only in this build. This screen manages the real email module: enabled state, sender identity,
                API credentials, sender allowlist, and test delivery from one controlled place.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge variant="info">Resend</StatusBadge>
              <StatusBadge variant={config?.receiveEnabled ? 'warning' : 'muted'}>{config?.receiveEnabled ? 'Inbound on' : 'Send-only'}</StatusBadge>
              <StatusBadge variant={deliveryReady ? 'ready' : 'warning'}>{deliveryReady ? 'Ready' : 'Needs setup'}</StatusBadge>
            </div>
          </div>
        </section>

        <section className="grid gap-3 xl:grid-cols-4">
          {[
            { label: 'Channel', value: draft?.enabled ? 'Enabled' : 'Disabled', note: draft?.enabled ? 'Email sending is allowed' : 'Messages will not send', variant: draft?.enabled ? 'ready' as const : 'muted' as const },
            { label: 'Sender', value: hasSender ? 'Configured' : 'Missing', note: draft?.address.trim() || 'Set the from address', variant: hasSender ? 'ready' as const : 'warning' as const },
            { label: 'API Key', value: hasApiKey ? 'Present' : 'Missing', note: hasApiKey ? 'Delivery can authenticate' : 'Add the Resend key', variant: hasApiKey ? 'ready' as const : 'warning' as const },
            { label: 'Allowlist', value: previewAllowFrom.length > 0 ? `${previewAllowFrom.length} entries` : 'Open', note: previewAllowFrom.length > 0 ? 'Restricted sender list' : 'No sender restrictions', variant: previewAllowFrom.length > 0 ? 'info' as const : 'muted' as const },
          ].map((item) => (
            <div key={item.label} className="rounded-xl border border-border bg-surface-100 px-4 py-3">
              <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">{item.label}</p>
              <div className="mt-2 flex items-baseline justify-between gap-2">
                <span className="text-2xl font-semibold text-zinc-100">{item.value}</span>
                <StatusBadge variant={item.variant}>{item.label}</StatusBadge>
              </div>
              <p className="mt-2 text-xs text-zinc-500 break-all">{item.note}</p>
            </div>
          ))}
        </section>

        <section className="grid gap-5 xl:grid-cols-[minmax(0,1.6fr)_360px]">
          <Card
            title="Email Management"
            actions={
              <div className="flex items-center gap-2">
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
            {!draft ? (
              <div className="space-y-3">
                {[1, 2, 3, 4].map((i) => (
                  <div key={i} className="h-14 rounded-md bg-surface-50/30 animate-pulse" />
                ))}
              </div>
            ) : (
              <div className="space-y-5">
                <div className="rounded-xl border border-border-subtle bg-surface-50/20 px-4 py-4">
                  <div className="flex items-center justify-between gap-4">
                    <div>
                      <p className="text-sm font-medium text-zinc-200">Email enabled</p>
                      <p className="mt-1 text-sm text-zinc-500">Toggle outbound email without relying on an immediate one-field write.</p>
                    </div>
                    <button
                      onClick={() => updateDraft('enabled', !draft.enabled)}
                      className={`relative h-7 w-12 rounded-full transition-colors duration-200 ${draft.enabled ? 'bg-brand' : 'bg-zinc-700'}`}
                      aria-label="Toggle email enabled"
                    >
                      <span className={`absolute top-1 h-5 w-5 rounded-full bg-white transition-transform duration-200 ${draft.enabled ? 'translate-x-6' : 'translate-x-1'}`} />
                    </button>
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <label className="space-y-1.5">
                    <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">Sender Email</span>
                    <input
                      type="email"
                      value={draft.address}
                      onChange={(e) => updateDraft('address', e.target.value)}
                      placeholder="support@example.com"
                      className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    />
                  </label>
                  <label className="space-y-1.5">
                    <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">Display Name</span>
                    <input
                      type="text"
                      value={draft.displayName}
                      onChange={(e) => updateDraft('displayName', e.target.value)}
                      placeholder="sciClaw"
                      className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    />
                  </label>
                  <label className="space-y-1.5">
                    <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">Resend API Key</span>
                    <input
                      type="password"
                      value={draft.apiKey}
                      onChange={(e) => updateDraft('apiKey', e.target.value)}
                      placeholder={config?.hasApiKey ? 'Configured. Enter a new key only to rotate it.' : 're_...'}
                      className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    />
                  </label>
                  <label className="space-y-1.5">
                    <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">Resend API URL</span>
                    <input
                      type="text"
                      value={draft.baseUrl}
                      onChange={(e) => updateDraft('baseUrl', e.target.value)}
                      placeholder="https://api.resend.com"
                      className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                    />
                  </label>
                </div>

                <label className="space-y-1.5 block">
                  <span className="text-xs uppercase tracking-[0.16em] text-zinc-500">Sender Allowlist</span>
                  <textarea
                    value={draft.allowFrom}
                    onChange={(e) => updateDraft('allowFrom', e.target.value)}
                    rows={5}
                    placeholder={'alice@example.com\n@example.org'}
                    className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                  />
                  <p className="text-xs leading-5 text-zinc-500">
                    One entry per line or comma separated. Use full addresses or domains like <span className="font-mono text-zinc-400">@example.org</span>.
                  </p>
                </label>

                <div className="flex flex-wrap items-center gap-2 border-t border-border-subtle pt-4">
                  <button
                    onClick={handleSave}
                    disabled={saving || !dirty}
                    className="inline-flex items-center gap-2 rounded-md bg-brand px-4 py-2 text-sm font-semibold text-surface-500 transition-colors hover:bg-brand-500 disabled:opacity-50"
                  >
                    {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                    Save email settings
                  </button>
                  <button
                    onClick={resetDraft}
                    disabled={!dirty || saving}
                    className="inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-surface-50 disabled:opacity-50"
                  >
                    <Undo2 className="w-4 h-4" />
                    Reset draft
                  </button>
                  {dirty ? <StatusBadge variant="warning">Unsaved changes</StatusBadge> : <StatusBadge variant="muted">Saved</StatusBadge>}
                </div>
              </div>
            )}
          </Card>

          <div className="space-y-5">
            <Card title="Module Status">
              {draft ? (
                <div className="space-y-4 text-sm">
                  <div className="rounded-xl border border-border-subtle bg-surface-50/20 px-4 py-3">
                    <div className="flex items-center gap-2 text-zinc-200">
                      <Mail className="h-4 w-4 text-brand" />
                      <span className="font-medium">Delivery readiness</span>
                    </div>
                    <p className="mt-2 text-zinc-400 leading-6">
                      {deliveryReady
                        ? 'The email module has what it needs to send mail now.'
                        : 'To send mail, keep email enabled, set a sender address, and provide a Resend API key.'}
                    </p>
                  </div>
                  <div className="rounded-xl border border-border-subtle bg-surface-50/20 px-4 py-3">
                    <p className="text-[11px] uppercase tracking-[0.18em] text-zinc-500">Current contract</p>
                    <div className="mt-3 space-y-2 text-zinc-300">
                      <div className="flex items-center justify-between gap-3">
                        <span>Provider</span>
                        <span className="font-mono text-zinc-400">resend</span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span>Inbound receive</span>
                        <span className="text-zinc-500">Disabled in this build</span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span>Receive mode</span>
                        <span className="font-mono text-zinc-400">{config?.receiveMode || 'poll'}</span>
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span>Poll interval</span>
                        <span className="font-mono text-zinc-400">{config?.pollIntervalSeconds || 30}s</span>
                      </div>
                    </div>
                  </div>
                  <div className="rounded-xl border border-border-subtle bg-surface-50/20 px-4 py-3">
                    <div className="flex items-center gap-2 text-zinc-200">
                      <ShieldCheck className="h-4 w-4 text-brand" />
                      <span className="font-medium">Sender controls</span>
                    </div>
                    <p className="mt-2 text-zinc-400 leading-6">
                      Use the sender allowlist now so inbound receive can be enabled later without opening the channel to arbitrary senders.
                    </p>
                  </div>
                </div>
              ) : (
                <div className="space-y-3">
                  {[1, 2, 3].map((i) => (
                    <div key={i} className="h-20 rounded-md bg-surface-50/30 animate-pulse" />
                  ))}
                </div>
              )}
            </Card>

            <Card title="Send Test Email">
              <div className="space-y-3">
                {!deliveryReady && (
                  <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 px-4 py-3 text-sm text-amber-300">
                    <div className="flex items-center gap-2">
                      <AlertTriangle className="h-4 w-4" />
                      <span>Save a valid sender address and API key before testing delivery.</span>
                    </div>
                  </div>
                )}
                <input
                  type="email"
                  value={testTo}
                  onChange={(e) => setTestTo(e.target.value)}
                  placeholder="recipient@example.com"
                  className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/40"
                  onKeyDown={(e) => e.key === 'Enter' && handleTest()}
                />
                <button
                  onClick={handleTest}
                  disabled={testing || !testTo.trim() || !deliveryReady}
                  className="inline-flex w-full items-center justify-center gap-2 rounded-md bg-brand px-4 py-2 text-sm font-semibold text-surface-500 transition-colors hover:bg-brand-500 disabled:opacity-50"
                >
                  {testing ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
                  {testing ? 'Sending test email...' : 'Send test email'}
                </button>
              </div>
            </Card>
          </div>
        </section>
      </main>
    </>
  );
}
