import { useState, useCallback } from 'react';

export function useFlash(duration = 4000) {
  const [flash, setFlash] = useState('');

  const showFlash = useCallback((msg: string) => {
    setFlash(msg);
    setTimeout(() => setFlash(''), duration);
  }, [duration]);

  return { flash, showFlash };
}

export function FlashBanner({ message }: { message: string }) {
  if (!message) return null;
  return (
    <div className="rounded-md bg-brand/10 border border-brand/20 px-4 py-2 text-sm text-brand animate-fade-in">
      {message}
    </div>
  );
}
