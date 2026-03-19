import { useState } from 'react';
import {
  Radio, Plus, Trash2, Settings as SettingsIcon, Check, X,
  Users as UsersIcon,
} from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import { useSnapshot } from '../hooks/useSnapshot';
import { addChannelUser, removeChannelUser, setupChannel } from '../lib/api';

type Channel = 'discord' | 'telegram';

function channelBadgeVariant(status: string) {
  if (status === 'ready') return 'ready' as const;
  if (status === 'open' || status === 'broken') return 'warning' as const;
  return 'muted' as const;
}

function channelStatusLabel(status: string) {
  if (status === 'ready') return 'Ready';
  if (status === 'open') return 'Open';
  if (status === 'broken') return 'Broken';
  return 'Off';
}

export default function ChannelsPage() {
  const { snapshot, refresh } = useSnapshot();
  const [activeChannel, setActiveChannel] = useState<Channel>('discord');
  const [addMode, setAddMode] = useState(false);
  const [addStep, setAddStep] = useState(0);
  const [addUserId, setAddUserId] = useState('');
  const [addUserName, setAddUserName] = useState('');
  const [setupMode, setSetupMode] = useState(false);
  const [setupStep, setSetupStep] = useState(0);
  const [setupToken, setSetupToken] = useState('');
  const [setupUserId, setSetupUserId] = useState('');
  const [setupUserName, setSetupUserName] = useState('');
  const [deleteUser, setDeleteUser] = useState<{ UserID: string; Username: string } | null>(null);
  const [flash, setFlash] = useState('');

  const chan = activeChannel === 'discord' ? snapshot.Discord : snapshot.Telegram;

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleAddUser = async () => {
    if (addStep === 0) {
      if (!addUserId.trim()) return;
      setAddStep(1);
      return;
    }
    try {
      await addChannelUser(activeChannel, addUserId.trim(), addUserName.trim());
      showFlash('User added');
      refresh();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setAddMode(false);
    setAddStep(0);
    setAddUserId('');
    setAddUserName('');
  };

  const handleRemoveUser = async () => {
    if (!deleteUser) return;
    try {
      await removeChannelUser(activeChannel, deleteUser.UserID);
      showFlash('User removed');
      refresh();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setDeleteUser(null);
  };

  const handleSetup = async () => {
    if (setupStep < 3) {
      setSetupStep((s) => s + 1);
      return;
    }
    try {
      await setupChannel(activeChannel, setupToken, setupUserId, setupUserName);
      showFlash('Channel configured');
      refresh();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setSetupMode(false);
    setSetupStep(0);
    setSetupToken('');
    setSetupUserId('');
    setSetupUserName('');
  };

  return (
    <>
      <TopBar title="Channels" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {/* Flash */}
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        {/* Channel tabs */}
        <div className="flex gap-2">
          {(['discord', 'telegram'] as const).map((ch) => {
            const s = ch === 'discord' ? snapshot.Discord : snapshot.Telegram;
            return (
              <button
                key={ch}
                onClick={() => { setActiveChannel(ch); setAddMode(false); setSetupMode(false); }}
                className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors duration-150 border ${
                  activeChannel === ch
                    ? 'border-brand/30 bg-brand/10 text-brand'
                    : 'border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50/50'
                }`}
              >
                <Radio className="w-4 h-4" />
                {ch === 'discord' ? 'Discord' : 'Telegram'}
                <StatusBadge variant={channelBadgeVariant(s.Status)} dot>
                  {channelStatusLabel(s.Status)}
                </StatusBadge>
              </button>
            );
          })}
        </div>

        {/* Channel status */}
        <Card title={`${activeChannel === 'discord' ? 'Discord' : 'Telegram'} Status`}>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
            <div>
              <p className="text-zinc-500 text-xs mb-1">Status</p>
              <StatusBadge variant={channelBadgeVariant(chan.Status)} dot>
                {channelStatusLabel(chan.Status)}
              </StatusBadge>
            </div>
            <div>
              <p className="text-zinc-500 text-xs mb-1">Enabled</p>
              <span className={chan.Enabled ? 'text-brand' : 'text-zinc-500'}>
                {chan.Enabled ? 'Yes' : 'No'}
              </span>
            </div>
            <div>
              <p className="text-zinc-500 text-xs mb-1">Token</p>
              <span className={chan.HasToken ? 'text-brand' : 'text-zinc-500'}>
                {chan.HasToken ? 'Set' : 'Missing'}
              </span>
            </div>
            <div>
              <p className="text-zinc-500 text-xs mb-1">Users</p>
              <span className="text-zinc-300">{chan.ApprovedUsers.length}</span>
            </div>
          </div>
        </Card>

        {/* Setup wizard */}
        {setupMode && (
          <Card title="Channel Setup">
            <div className="space-y-4">
              {setupStep === 0 && (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Bot Token</label>
                  <input
                    type="password"
                    value={setupToken}
                    onChange={(e) => setSetupToken(e.target.value)}
                    placeholder={activeChannel === 'discord' ? 'Discord bot token' : 'Telegram bot token'}
                    className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                    autoFocus
                  />
                </div>
              )}
              {setupStep === 1 && (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Your User ID</label>
                  <input
                    type="text"
                    value={setupUserId}
                    onChange={(e) => setSetupUserId(e.target.value)}
                    placeholder={activeChannel === 'discord' ? 'e.g. 123456789012345678' : 'e.g. 987654321'}
                    className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                    autoFocus
                  />
                </div>
              )}
              {setupStep === 2 && (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Display Name (optional)</label>
                  <input
                    type="text"
                    value={setupUserName}
                    onChange={(e) => setSetupUserName(e.target.value)}
                    placeholder="e.g. Dr. Smith"
                    className="w-full rounded-md border border-border bg-surface-100 px-3 py-2 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                    autoFocus
                  />
                </div>
              )}
              {setupStep === 3 && (
                <div className="text-sm text-zinc-300 space-y-2">
                  <p>Review your setup:</p>
                  <div className="bg-surface-50/30 rounded-md p-3 font-mono text-xs space-y-1">
                    <p>Token: {'•'.repeat(12)}</p>
                    <p>User ID: {setupUserId}</p>
                    {setupUserName && <p>Name: {setupUserName}</p>}
                  </div>
                </div>
              )}
              <div className="flex gap-2">
                <button
                  onClick={() => { setSetupMode(false); setSetupStep(0); }}
                  className="px-3 py-1.5 text-xs rounded-md border border-border text-zinc-400 hover:bg-surface-50 transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleSetup}
                  className="px-3 py-1.5 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
                >
                  {setupStep < 3 ? 'Next' : 'Confirm'}
                </button>
              </div>
            </div>
          </Card>
        )}

        {/* Users list */}
        <Card
          title="Approved Users"
          actions={
            <div className="flex gap-2">
              {!chan.HasToken && (
                <button
                  onClick={() => setSetupMode(true)}
                  className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md border border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50 transition-colors"
                >
                  <SettingsIcon className="w-3 h-3" />
                  Setup
                </button>
              )}
              <button
                onClick={() => setAddMode(true)}
                className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
              >
                <Plus className="w-3 h-3" />
                Add User
              </button>
            </div>
          }
        >
          {/* Add user form */}
          {addMode && (
            <div className="mb-4 p-3 rounded-md border border-brand/20 bg-brand/5 space-y-3 animate-fade-in">
              {addStep === 0 ? (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">User ID</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={addUserId}
                      onChange={(e) => setAddUserId(e.target.value)}
                      placeholder={activeChannel === 'discord' ? 'Discord user ID' : 'Telegram user ID'}
                      className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                      autoFocus
                      onKeyDown={(e) => e.key === 'Enter' && handleAddUser()}
                    />
                    <button onClick={handleAddUser} className="p-1.5 rounded-md bg-brand text-surface-500">
                      <Check className="w-4 h-4" />
                    </button>
                    <button onClick={() => { setAddMode(false); setAddUserId(''); }} className="p-1.5 rounded-md border border-border text-zinc-400">
                      <X className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              ) : (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">Display Name (optional)</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={addUserName}
                      onChange={(e) => setAddUserName(e.target.value)}
                      placeholder="e.g. Dr. Smith"
                      className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                      autoFocus
                      onKeyDown={(e) => e.key === 'Enter' && handleAddUser()}
                    />
                    <button onClick={handleAddUser} className="p-1.5 rounded-md bg-brand text-surface-500">
                      <Check className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          {chan.ApprovedUsers.length === 0 ? (
            <EmptyState
              icon={UsersIcon}
              title="No approved users"
              description="Add users who are allowed to interact with this channel."
            />
          ) : (
            <div className="divide-y divide-border-subtle">
              {chan.ApprovedUsers.map((u) => (
                <div
                  key={u.UserID}
                  className="flex items-center justify-between py-2.5 group"
                >
                  <div className="flex items-center gap-3">
                    <div className="w-7 h-7 rounded-full bg-surface-50 flex items-center justify-center text-xs text-zinc-500 font-mono">
                      {u.Username ? u.Username[0].toUpperCase() : '#'}
                    </div>
                    <div>
                      <p className="text-sm text-zinc-300">{u.Username || u.UserID}</p>
                      {u.Username && <p className="text-xs text-zinc-600 font-mono">{u.UserID}</p>}
                    </div>
                  </div>
                  <button
                    onClick={() => setDeleteUser(u)}
                    className="p-1 rounded hover:bg-red-500/10 text-zinc-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-all duration-150"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </Card>
      </main>

      <ConfirmDialog
        open={!!deleteUser}
        title="Remove User"
        message={`Remove ${deleteUser?.Username || deleteUser?.UserID} from ${activeChannel}?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemoveUser}
        onCancel={() => setDeleteUser(null)}
      />
    </>
  );
}
