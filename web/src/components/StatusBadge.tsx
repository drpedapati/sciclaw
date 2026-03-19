type Variant = 'ready' | 'error' | 'warning' | 'info' | 'muted';

const styles: Record<Variant, string> = {
  ready:   'bg-brand/15 text-brand',
  error:   'bg-red-500/15 text-red-400',
  warning: 'bg-amber-500/15 text-amber-400',
  info:    'bg-cyan-500/15 text-cyan-400',
  muted:   'bg-zinc-500/15 text-zinc-400',
};

export default function StatusBadge({
  variant = 'muted',
  children,
  dot,
}: {
  variant?: Variant;
  children: React.ReactNode;
  dot?: boolean;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium ${styles[variant]}`}
    >
      {dot && (
        <span
          className={`w-1.5 h-1.5 rounded-full ${
            variant === 'ready' ? 'bg-brand animate-pulse-dot' :
            variant === 'error' ? 'bg-red-400' :
            variant === 'warning' ? 'bg-amber-400' :
            variant === 'info' ? 'bg-cyan-400' :
            'bg-zinc-400'
          }`}
        />
      )}
      {children}
    </span>
  );
}
