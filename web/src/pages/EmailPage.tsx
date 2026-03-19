import { useState, useEffect } from 'react';
import { Mail, Send, RefreshCw, Loader2 } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import { getEmailConfig, updateEmailConfig, sendTestEmail, type EmailConfig } from '../lib/api';

interface FieldDef {
  key: string;
  label: string;
  type: 'toggle' | 'text' | 'password';
  configKey: keyof EmailConfig | 'apiKey' | 'testRecipient';
}

const fields: FieldDef[] = [
  { key: 'enabled', label: 'Enabled', type: 'toggle', configKey: 'enabled' },
  { key: 'address', label: 'Sender Email', type: 'text', configKey: 'address' },
  { key: 'displayName', label: 'Display Name', type: 'text', configKey: 'displayName' },
  { key: 'apiKey', label: 'API Key', type: 'password', configKey: 'apiKey' },
  { key: 'provider', label: 'Provider', type: 'text', configKey: 'provider' },
  { key: 'baseUrl', label: 'Base URL', type: 'text', configKey: 'baseUrl' },
];

export default function EmailPage() {
  const [config, setConfig] = useState<EmailConfig | null>(null);
  const [editField, setEditField] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [testTo, setTestTo] = useState('');
  const [flash, setFlash] = useState('');
  const [loading, setLoading] = useState(false);
  const [testing, setTesting] = useState(false);

  const fetchData = async () => {
    try {
      const data = await getEmailConfig();
      setConfig(data);
    } catch { /* */ }
  };

  useEffect(() => { fetchData(); }, []);

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleSave = async (key: string, value: unknown) => {
    setLoading(true);
    try {
      await updateEmailConfig({ [key]: value });
      showFlash('Saved');
      await fetchData();
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setLoading(false);
      setEditField(null);
    }
  };

  const handleTest = async () => {
    if (!testTo.trim()) return;
    setTesting(true);
    try {
      const result = await sendTestEmail(testTo.trim());
      showFlash(result.ok ? 'Test email sent' : result.output);
    } catch (e) {
      showFlash(`Error: ${e}`);
    } finally {
      setTesting(false);
    }
  };

  const getDisplayValue = (field: FieldDef): string => {
    if (!config) return '—';
    if (field.key === 'enabled') return config.enabled ? 'On' : 'Off';
    if (field.key === 'apiKey') return config.hasApiKey ? '••••••••' : 'Not set';
    return (config as Record<string, unknown>)[field.key] as string || '—';
  };

  return (
    <>
      <TopBar title="Email" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <Card
          title="Email Configuration"
          actions={
            <button onClick={fetchData} className="p-1 rounded hover:bg-surface-50 text-zinc-500 hover:text-zinc-300 transition-colors">
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          }
        >
          {!config ? (
            <div className="space-y-3">
              {[1, 2, 3, 4, 5].map((i) => (
                <div key={i} className="h-10 rounded-md bg-surface-50/30 animate-pulse" />
              ))}
            </div>
          ) : (
            <div className="divide-y divide-border-subtle">
              {fields.map((field) => (
                <div
                  key={field.key}
                  className="flex items-center justify-between py-3 group"
                >
                  <span className="text-sm text-zinc-500 w-32 flex-shrink-0">{field.label}</span>
                  {editField === field.key ? (
                    <div className="flex items-center gap-2 flex-1 justify-end">
                      <input
                        type={field.type === 'password' ? 'password' : 'text'}
                        value={editValue}
                        onChange={(e) => setEditValue(e.target.value)}
                        className="w-64 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 focus:outline-none focus:ring-1 focus:ring-brand/50"
                        autoFocus
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') handleSave(field.configKey, editValue);
                          if (e.key === 'Escape') setEditField(null);
                        }}
                      />
                      <button
                        onClick={() => handleSave(field.configKey, editValue)}
                        disabled={loading}
                        className="px-2 py-1 text-xs rounded bg-brand text-surface-500 hover:bg-brand-500 transition-colors"
                      >
                        Save
                      </button>
                    </div>
                  ) : field.type === 'toggle' ? (
                    <button
                      onClick={() => handleSave(field.configKey, !config.enabled)}
                      className={`relative w-9 h-5 rounded-full transition-colors duration-200 ${
                        config.enabled ? 'bg-brand' : 'bg-zinc-700'
                      }`}
                    >
                      <span
                        className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform duration-200 ${
                          config.enabled ? 'translate-x-4' : 'translate-x-0.5'
                        }`}
                      />
                    </button>
                  ) : (
                    <button
                      onClick={() => {
                        setEditField(field.key);
                        setEditValue(field.key === 'apiKey' ? '' : getDisplayValue(field));
                      }}
                      className="text-sm text-zinc-300 hover:text-zinc-100 font-mono text-right transition-colors cursor-pointer"
                    >
                      {getDisplayValue(field)}
                    </button>
                  )}
                </div>
              ))}

              {/* Allow-from */}
              <div className="py-3">
                <span className="text-sm text-zinc-500 block mb-2">Allow From</span>
                <div className="flex flex-wrap gap-1.5">
                  {config.allowFrom.length > 0 ? (
                    config.allowFrom.map((email) => (
                      <span key={email} className="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-surface-50 text-zinc-400 border border-border">
                        {email}
                      </span>
                    ))
                  ) : (
                    <span className="text-xs text-zinc-600">No restrictions</span>
                  )}
                </div>
              </div>
            </div>
          )}
        </Card>

        {/* Test email */}
        <Card title="Send Test Email">
          <div className="flex gap-3">
            <input
              type="email"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
              placeholder="recipient@example.com"
              className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
              onKeyDown={(e) => e.key === 'Enter' && handleTest()}
            />
            <button
              onClick={handleTest}
              disabled={testing || !testTo.trim()}
              className="flex items-center gap-2 px-4 py-2 text-sm rounded-md bg-brand text-surface-500 hover:bg-brand-500 disabled:opacity-50 transition-colors font-medium"
            >
              {testing ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
              {testing ? 'Sending...' : 'Send'}
            </button>
          </div>
        </Card>
      </main>
    </>
  );
}
