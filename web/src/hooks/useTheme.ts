import { useState, useEffect, useCallback } from 'react';

export interface Theme {
  id: string;
  name: string;
  emoji: string;
  brand: string;        // main accent
  brand500: string;     // hover
  brandFaint: string;   // 15% opacity bg
  selection: string;    // text selection
}

export const themes: Theme[] = [
  {
    id: 'emerald',
    name: 'Emerald',
    emoji: '\u{1F48E}',
    brand: '#3ecf8e',
    brand500: '#2da36d',
    brandFaint: 'rgba(62,207,142,0.15)',
    selection: 'rgba(62,207,142,0.25)',
  },
  {
    id: 'amethyst',
    name: 'Amethyst',
    emoji: '\u{1F52E}',
    brand: '#a78bfa',
    brand500: '#8b6ff0',
    brandFaint: 'rgba(167,139,250,0.15)',
    selection: 'rgba(167,139,250,0.25)',
  },
  {
    id: 'coral',
    name: 'Coral',
    emoji: '\u{1F3DD}',
    brand: '#f97066',
    brand500: '#e5534b',
    brandFaint: 'rgba(249,112,102,0.15)',
    selection: 'rgba(249,112,102,0.25)',
  },
  {
    id: 'ocean',
    name: 'Ocean',
    emoji: '\u{1F30A}',
    brand: '#38bdf8',
    brand500: '#0ea5e9',
    brandFaint: 'rgba(56,189,248,0.15)',
    selection: 'rgba(56,189,248,0.25)',
  },
  {
    id: 'amber',
    name: 'Amber',
    emoji: '\u{1F525}',
    brand: '#fbbf24',
    brand500: '#d4a017',
    brandFaint: 'rgba(251,191,36,0.15)',
    selection: 'rgba(251,191,36,0.25)',
  },
  {
    id: 'rose',
    name: 'Rose',
    emoji: '\u{1F339}',
    brand: '#f472b6',
    brand500: '#e44d95',
    brandFaint: 'rgba(244,114,182,0.15)',
    selection: 'rgba(244,114,182,0.25)',
  },
  {
    id: 'mint',
    name: 'Mint',
    emoji: '\u{1F343}',
    brand: '#34d399',
    brand500: '#10b981',
    brandFaint: 'rgba(52,211,153,0.15)',
    selection: 'rgba(52,211,153,0.25)',
  },
  {
    id: 'lavender',
    name: 'Lavender',
    emoji: '\u{1F338}',
    brand: '#c4b5fd',
    brand500: '#a78bfa',
    brandFaint: 'rgba(196,181,253,0.15)',
    selection: 'rgba(196,181,253,0.25)',
  },
];

const STORAGE_KEY = 'sciclaw-theme';

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  root.style.setProperty('--color-brand', theme.brand);
  root.style.setProperty('--color-brand-500', theme.brand500);
  root.style.setProperty('--theme-brand-faint', theme.brandFaint);
  root.style.setProperty('--theme-selection', theme.selection);
  root.setAttribute('data-theme', theme.id);
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    return themes.find(t => t.id === saved) || themes[0];
  });

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  const setTheme = useCallback((t: Theme) => {
    setThemeState(t);
    localStorage.setItem(STORAGE_KEY, t.id);
  }, []);

  return { theme, setTheme, themes };
}
