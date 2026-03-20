import { useState } from 'react';
import { Users as UsersIcon, Plus, Trash2, Pencil, Check, X, Radio } from 'lucide-react';
import TopBar from '../components/TopBar';
import Card from '../components/Card';
import StatusBadge from '../components/StatusBadge';
import ConfirmDialog from '../components/ConfirmDialog';
import EmptyState from '../components/EmptyState';
import { useSnapshot } from '../hooks/useSnapshot';
import { addChannelUser, removeChannelUser } from '../lib/api';

interface UserRow {
  channel: 'discord' | 'telegram';
  id: string;
  name: string;
}

export default function UsersPage() {
  const { snapshot, refresh } = useSnapshot();
  const [addMode, setAddMode] = useState(false);
  const [addStep, setAddStep] = useState(0); // 0=channel, 1=id, 2=name
  const [addChannel, setAddChannel] = useState<'discord' | 'telegram'>('discord');
  const [addUserId, setAddUserId] = useState('');
  const [addUserName, setAddUserName] = useState('');
  const [editUser, setEditUser] = useState<UserRow | null>(null);
  const [editName, setEditName] = useState('');
  const [deleteUser, setDeleteUser] = useState<UserRow | null>(null);
  const [flash, setFlash] = useState('');

  // Combine users from both channels
  const users: UserRow[] = [
    ...snapshot.Discord.ApprovedUsers.map((u) => ({ channel: 'discord' as const, id: u.UserID, name: u.Username })),
    ...snapshot.Telegram.ApprovedUsers.map((u) => ({ channel: 'telegram' as const, id: u.UserID, name: u.Username })),
  ];

  const showFlash = (msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), 4000);
  };

  const handleAdd = async () => {
    if (addStep === 0) { setAddStep(1); return; }
    if (addStep === 1) {
      if (!addUserId.trim()) return;
      setAddStep(2);
      return;
    }
    try {
      await addChannelUser(addChannel, addUserId.trim(), addUserName.trim());
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

  const handleRemove = async () => {
    if (!deleteUser) return;
    try {
      await removeChannelUser(deleteUser.channel, deleteUser.id);
      showFlash('User removed');
      refresh();
    } catch (e) {
      showFlash(`Error: ${e}`);
    }
    setDeleteUser(null);
  };

  return (
    <>
      <TopBar title="Users" />
      <main className="flex-1 overflow-auto p-6 space-y-5 animate-fade-in">
        {flash && (
          <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
            {flash}
          </div>
        )}

        <Card
          title={`All Users (${users.length})`}
          className="overflow-hidden"
          actions={
            <button
              onClick={() => { setAddMode(true); setAddStep(0); }}
              className="flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md bg-brand text-surface-500 hover:bg-brand-500 transition-colors font-medium"
            >
              <Plus className="w-3 h-3" />
              Add User
            </button>
          }
        >
          {/* Add wizard */}
          {addMode && (
            <div className="mb-4 p-3 rounded-md border border-brand/20 bg-brand/5 space-y-3 animate-fade-in">
              {addStep === 0 && (
                <div>
                  <label className="text-xs text-zinc-500 block mb-2">Channel</label>
                  <div className="flex gap-2">
                    {(['discord', 'telegram'] as const).map((ch) => (
                      <button
                        key={ch}
                        onClick={() => { setAddChannel(ch); setAddStep(1); }}
                        className={`flex items-center gap-2 px-3 py-2 rounded-md text-sm border transition-colors ${
                          addChannel === ch
                            ? 'border-brand/30 bg-brand/10 text-brand'
                            : 'border-border text-zinc-400 hover:text-zinc-200 hover:bg-surface-50'
                        }`}
                      >
                        <Radio className="w-3.5 h-3.5" />
                        {ch === 'discord' ? 'Discord' : 'Telegram'}
                      </button>
                    ))}
                    <button onClick={() => setAddMode(false)} className="ml-auto p-1.5 rounded text-zinc-500 hover:text-zinc-300">
                      <X className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              )}
              {addStep === 1 && (
                <div>
                  <label className="text-xs text-zinc-500 block mb-1">User ID ({addChannel})</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={addUserId}
                      onChange={(e) => setAddUserId(e.target.value)}
                      placeholder={addChannel === 'discord' ? 'e.g. 123456789012345678' : 'e.g. 987654321'}
                      className="flex-1 rounded-md border border-border bg-surface-100 px-3 py-1.5 text-sm text-zinc-200 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-brand/50"
                      autoFocus
                      onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
                    />
                    <button onClick={handleAdd} className="px-3 py-1.5 text-xs rounded bg-brand text-surface-500 font-medium">Next</button>
                  </div>
                </div>
              )}
              {addStep === 2 && (
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
                      onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
                    />
                    <button onClick={handleAdd} className="px-3 py-1.5 text-xs rounded bg-brand text-surface-500 font-medium">Add</button>
                  </div>
                </div>
              )}
            </div>
          )}

          {users.length === 0 ? (
            <EmptyState
              icon={UsersIcon}
              title="No users configured"
              description="Add approved users from Discord or Telegram channels."
            />
          ) : (
            <div className="overflow-x-auto rounded-md border border-border-subtle bg-surface-50/20">
              <table className="w-full table-fixed">
                <thead>
                  <tr className="border-b border-border bg-surface-50/40">
                    <th className="w-28 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Channel</th>
                    <th className="w-52 text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">User ID</th>
                    <th className="text-left px-3 py-2 text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Name</th>
                    <th className="w-24 px-3 py-2 text-right text-[11px] uppercase text-zinc-500 font-semibold tracking-[0.18em]">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {users.map((u) => {
                    const isEditing = editUser?.id === u.id && editUser?.channel === u.channel;
                    return (
                      <tr key={`${u.channel}-${u.id}`} className="bg-transparent transition-colors hover:bg-surface-50/45">
                        <td className="px-3 py-2 align-middle">
                          <StatusBadge variant={u.channel === 'discord' ? 'info' : 'ready'}>
                            {u.channel === 'discord' ? 'Discord' : 'Telegram'}
                          </StatusBadge>
                        </td>
                        <td className="px-3 py-2 align-middle text-[12px] text-zinc-400 font-mono tabular-nums truncate">{u.id}</td>
                        <td className="px-3 py-2 align-middle text-sm text-zinc-300">
                          {isEditing ? (
                            <div className="flex items-center gap-1.5">
                              <input
                                type="text"
                                value={editName}
                                onChange={(e) => setEditName(e.target.value)}
                                className="w-40 rounded border border-border bg-surface-100 px-2 py-1 text-sm text-zinc-200 focus:outline-none focus:ring-1 focus:ring-brand/50"
                                autoFocus
                                onKeyDown={(e) => {
                                  if (e.key === 'Enter') setEditUser(null);
                                  if (e.key === 'Escape') setEditUser(null);
                                }}
                              />
                              <button
                                onClick={() => setEditUser(null)}
                                className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border-subtle text-zinc-500 transition-colors hover:border-border hover:bg-surface-50 hover:text-zinc-200"
                                aria-label={`Stop editing ${u.name || u.id}`}
                              >
                                <Check className="w-3.5 h-3.5" />
                              </button>
                            </div>
                          ) : (
                            <span className={`block truncate text-sm ${u.name ? 'text-zinc-200' : 'text-zinc-600'}`}>
                              {u.name || '—'}
                            </span>
                          )}
                        </td>
                        <td className="px-3 py-2 align-middle">
                          <div className="flex items-center justify-end gap-1">
                            <button
                              onClick={() => { setEditUser(u); setEditName(u.name); }}
                              className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-zinc-600 transition-colors hover:border-border hover:bg-surface-50 hover:text-zinc-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30"
                              aria-label={`Edit ${u.name || u.id}`}
                            >
                              <Pencil className="w-3.5 h-3.5" />
                            </button>
                            <button
                              onClick={() => setDeleteUser(u)}
                              className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-zinc-600 transition-colors hover:border-red-500/25 hover:bg-red-500/10 hover:text-red-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500/25"
                              aria-label={`Remove ${u.name || u.id}`}
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </main>

      <ConfirmDialog
        open={!!deleteUser}
        title="Remove User"
        message={`Remove ${deleteUser?.name || deleteUser?.id} from ${deleteUser?.channel}?`}
        confirmLabel="Remove"
        danger
        onConfirm={handleRemove}
        onCancel={() => setDeleteUser(null)}
      />
    </>
  );
}
