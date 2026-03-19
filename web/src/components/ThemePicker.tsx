import { useState } from 'react';
import { Palette } from 'lucide-react';
import { useTheme } from '../hooks/useTheme';

export default function ThemePicker() {
  const { theme, setTheme, themes } = useTheme();
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 w-full px-3 py-2 rounded-md text-xs text-zinc-500 hover:text-zinc-300 hover:bg-surface-50/50 transition-colors duration-150"
        title="Change theme"
      >
        <Palette className="w-3.5 h-3.5" />
        <span className="flex-1 text-left">{theme.name}</span>
        <span
          className="w-3 h-3 rounded-full border border-white/10"
          style={{ background: theme.brand }}
        />
      </button>

      {open && (
        <>
          {/* Backdrop */}
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />

          {/* Popover */}
          <div className="absolute bottom-full left-0 mb-2 w-52 rounded-lg border border-border bg-surface-200 shadow-xl z-50 animate-fade-in overflow-hidden">
            <div className="px-3 py-2 border-b border-border-subtle">
              <p className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">Theme</p>
            </div>
            <div className="p-1.5 grid grid-cols-2 gap-1">
              {themes.map((t) => (
                <button
                  key={t.id}
                  onClick={() => { setTheme(t); setOpen(false); }}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-md text-xs transition-all duration-150 ${
                    theme.id === t.id
                      ? 'bg-white/5 ring-1 ring-white/10'
                      : 'hover:bg-surface-50/50'
                  }`}
                >
                  <span
                    className={`w-3 h-3 rounded-full transition-transform duration-200 ${
                      theme.id === t.id ? 'scale-125' : ''
                    }`}
                    style={{ background: t.brand }}
                  />
                  <span className={theme.id === t.id ? 'text-zinc-200' : 'text-zinc-500'}>
                    {t.name}
                  </span>
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
