import { NavLink } from 'react-router-dom';
import {
  Home, MessageSquare, Radio, Mail, Users, LogIn,
  Stethoscope, Server, Cpu, Brain, Puzzle, Clock,
  GitBranch, Settings, Activity,
} from 'lucide-react';
import { useSnapshot } from '../hooks/useSnapshot';
import ThemePicker from './ThemePicker';

const navItems = [
  { to: '/',         icon: Home,          label: 'Home' },
  { to: '/chat',     icon: MessageSquare, label: 'Chat' },
  { to: '/channels', icon: Radio,         label: 'Channels' },
  { to: '/email',    icon: Mail,          label: 'Email' },
  { to: '/users',    icon: Users,         label: 'Users' },
  { to: '/routing',  icon: GitBranch,     label: 'Routing' },
  { to: '/models',   icon: Cpu,           label: 'Models' },
  { to: '/phi',      icon: Brain,         label: 'PHI' },
  { to: '/skills',   icon: Puzzle,        label: 'Skills' },
  { to: '/schedule', icon: Clock,         label: 'Schedule' },
] as const;

const adminItems = [
  { to: '/gateway',  icon: Server,        label: 'Gateway' },
  { to: '/health',   icon: Stethoscope,   label: 'Health' },
  { to: '/login',    icon: LogIn,         label: 'Login' },
  { to: '/settings', icon: Settings,      label: 'Settings' },
] as const;

function StatusDot({ running }: { running: boolean }) {
  return (
    <span
      className={`inline-block w-2 h-2 rounded-full ${
        running ? 'bg-brand animate-pulse-dot' : 'bg-zinc-600'
      }`}
    />
  );
}

export default function Sidebar() {
  const { snapshot } = useSnapshot();

  return (
    <aside className="w-56 flex-shrink-0 flex flex-col bg-surface-200 border-r border-border h-screen sticky top-0">
      {/* Logo */}
      <div className="h-14 flex items-center gap-2.5 px-5 border-b border-border">
        <Activity className="w-5 h-5 text-brand" />
        <span className="text-base font-semibold text-zinc-100 tracking-tight">sciClaw</span>
      </div>

      {/* Primary nav */}
      <nav className="flex-1 py-3 px-3 space-y-0.5 overflow-y-auto">
        <p className="px-3 pt-2 pb-1.5 text-[10px] font-medium uppercase tracking-wider text-zinc-600">
          Workspace
        </p>
        {navItems.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-2.5 px-3 py-1.5 rounded-md text-sm transition-colors duration-150 ${
                isActive
                  ? 'bg-brand/10 text-brand border-l-2 border-brand -ml-px'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-surface-50/50'
              }`
            }
          >
            <Icon className="w-4 h-4 flex-shrink-0" />
            {label}
          </NavLink>
        ))}

        <p className="px-3 pt-4 pb-1.5 text-[10px] font-medium uppercase tracking-wider text-zinc-600">
          Admin
        </p>
        {adminItems.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              `flex items-center gap-2.5 px-3 py-1.5 rounded-md text-sm transition-colors duration-150 ${
                isActive
                  ? 'bg-brand/10 text-brand border-l-2 border-brand -ml-px'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-surface-50/50'
              }`
            }
          >
            <Icon className="w-4 h-4 flex-shrink-0" />
            {label}
          </NavLink>
        ))}
      </nav>

      {/* Theme picker */}
      <div className="px-3 py-2 border-t border-border">
        <ThemePicker />
      </div>

      {/* Bottom status */}
      <div className="px-4 py-3 border-t border-border space-y-2">
        <div className="flex items-center justify-between text-xs">
          <span className="text-zinc-500">Gateway</span>
          <span className="flex items-center gap-1.5">
            <StatusDot running={snapshot.ServiceRunning} />
            <span className={snapshot.ServiceRunning ? 'text-brand' : 'text-zinc-500'}>
              {snapshot.ServiceRunning ? 'Running' : 'Stopped'}
            </span>
          </span>
        </div>
        {snapshot.ActiveModel && (
          <div className="flex items-center justify-between text-xs">
            <span className="text-zinc-500">Model</span>
            <span className="text-zinc-400 font-mono text-[11px] truncate max-w-[100px]">
              {snapshot.ActiveModel}
            </span>
          </div>
        )}
      </div>
    </aside>
  );
}
