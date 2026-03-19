import { useState, useLayoutEffect, useCallback } from 'react';

export interface Theme {
  id: string;
  name: string;
  emoji: string;
  scheme: 'dark' | 'light';
  brand: string;        // main accent
  brand500: string;     // hover
  brandFaint: string;   // 15% opacity bg
  selection: string;    // text selection
  surface50: string;
  surface100: string;
  surface200: string;
  surface300: string;
  surface400: string;
  surface500: string;
  border: string;
  borderSubtle: string;
  borderEmphasis: string;
  textStrong: string;
  textBase: string;
  textMuted: string;
  textSubtle: string;
  scrollbar: string;
  scrollbarHover: string;
}

type ThemePalette = Omit<
  Theme,
  'id' | 'name' | 'emoji' | 'brand' | 'brand500' | 'brandFaint' | 'selection'
>;

const darkPalette: ThemePalette = {
  scheme: 'dark' as const,
  surface50: '#2a2a2a',
  surface100: '#232323',
  surface200: '#1c1c1c',
  surface300: '#171717',
  surface400: '#111111',
  surface500: '#0a0a0a',
  border: '#2e2e2e',
  borderSubtle: '#232323',
  borderEmphasis: '#3e3e3e',
  textStrong: '#f5f5f5',
  textBase: '#d4d4d8',
  textMuted: '#a1a1aa',
  textSubtle: '#71717a',
  scrollbar: '#3f3f46',
  scrollbarHover: '#52525b',
};

const lightPaperPalette: ThemePalette = {
  scheme: 'light' as const,
  surface50: '#ebe5db',
  surface100: '#fcfaf4',
  surface200: '#f3ede1',
  surface300: '#ede6d8',
  surface400: '#ddd3c0',
  surface500: '#1f261f',
  border: '#d6ccbc',
  borderSubtle: '#e7dfd2',
  borderEmphasis: '#beb49f',
  textStrong: '#1f261f',
  textBase: '#384033',
  textMuted: '#6b7368',
  textSubtle: '#8a9188',
  scrollbar: '#b3aa9c',
  scrollbarHover: '#8d8478',
};

const lightMistPalette: ThemePalette = {
  scheme: 'light' as const,
  surface50: '#dfe9f2',
  surface100: '#fbfdff',
  surface200: '#eef3f8',
  surface300: '#e6edf5',
  surface400: '#d0dcea',
  surface500: '#162131',
  border: '#c6d3e1',
  borderSubtle: '#dde7f0',
  borderEmphasis: '#aebed0',
  textStrong: '#162131',
  textBase: '#32445a',
  textMuted: '#617186',
  textSubtle: '#8895a8',
  scrollbar: '#afbcc9',
  scrollbarHover: '#8594a6',
};

const lightStudioPalette: ThemePalette = {
  scheme: 'light' as const,
  surface50: '#e3ebe3',
  surface100: '#fbfdf8',
  surface200: '#eef3ea',
  surface300: '#e6ece0',
  surface400: '#ced8c7',
  surface500: '#1e241f',
  border: '#c6d1bf',
  borderSubtle: '#dfe6d8',
  borderEmphasis: '#adb9a7',
  textStrong: '#1e241f',
  textBase: '#334034',
  textMuted: '#667166',
  textSubtle: '#899288',
  scrollbar: '#a8b5a5',
  scrollbarHover: '#7f8d7d',
};

function makeTheme(theme: Theme): Theme {
  return theme;
}

export const themes: Theme[] = [
  makeTheme({
    id: 'emerald',
    name: 'Emerald',
    emoji: '\u{1F48E}',
    brand: '#3ecf8e',
    brand500: '#2da36d',
    brandFaint: 'rgba(62,207,142,0.15)',
    selection: 'rgba(62,207,142,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'amethyst',
    name: 'Amethyst',
    emoji: '\u{1F52E}',
    brand: '#a78bfa',
    brand500: '#8b6ff0',
    brandFaint: 'rgba(167,139,250,0.15)',
    selection: 'rgba(167,139,250,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'coral',
    name: 'Coral',
    emoji: '\u{1F3DD}',
    brand: '#f97066',
    brand500: '#e5534b',
    brandFaint: 'rgba(249,112,102,0.15)',
    selection: 'rgba(249,112,102,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'ocean',
    name: 'Ocean',
    emoji: '\u{1F30A}',
    brand: '#38bdf8',
    brand500: '#0ea5e9',
    brandFaint: 'rgba(56,189,248,0.15)',
    selection: 'rgba(56,189,248,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'amber',
    name: 'Amber',
    emoji: '\u{1F525}',
    brand: '#fbbf24',
    brand500: '#d4a017',
    brandFaint: 'rgba(251,191,36,0.15)',
    selection: 'rgba(251,191,36,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'rose',
    name: 'Rose',
    emoji: '\u{1F339}',
    brand: '#f472b6',
    brand500: '#e44d95',
    brandFaint: 'rgba(244,114,182,0.15)',
    selection: 'rgba(244,114,182,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'mint',
    name: 'Mint',
    emoji: '\u{1F343}',
    brand: '#34d399',
    brand500: '#10b981',
    brandFaint: 'rgba(52,211,153,0.15)',
    selection: 'rgba(52,211,153,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'lavender',
    name: 'Lavender',
    emoji: '\u{1F338}',
    brand: '#c4b5fd',
    brand500: '#a78bfa',
    brandFaint: 'rgba(196,181,253,0.15)',
    selection: 'rgba(196,181,253,0.25)',
    ...darkPalette,
  }),
  makeTheme({
    id: 'linen',
    name: 'Linen',
    emoji: '\u{2600}\u{fe0f}',
    brand: '#16856b',
    brand500: '#0f6d57',
    brandFaint: 'rgba(22,133,107,0.12)',
    selection: 'rgba(22,133,107,0.2)',
    ...lightPaperPalette,
  }),
  makeTheme({
    id: 'mist',
    name: 'Mist',
    emoji: '\u{2601}\u{fe0f}',
    brand: '#3578b7',
    brand500: '#275d90',
    brandFaint: 'rgba(53,120,183,0.12)',
    selection: 'rgba(53,120,183,0.2)',
    ...lightMistPalette,
  }),
  makeTheme({
    id: 'studio',
    name: 'Studio',
    emoji: '\u{1F33F}',
    brand: '#6f8f3c',
    brand500: '#56702d',
    brandFaint: 'rgba(111,143,60,0.12)',
    selection: 'rgba(111,143,60,0.2)',
    ...lightStudioPalette,
  }),
];

const STORAGE_KEY = 'sciclaw-theme';

function applyTheme(theme: Theme) {
  const root = document.documentElement;
  root.style.setProperty('--color-brand', theme.brand);
  root.style.setProperty('--color-brand-500', theme.brand500);
  root.style.setProperty('--theme-brand-faint', theme.brandFaint);
  root.style.setProperty('--theme-selection', theme.selection);
  root.style.setProperty('--color-surface-50', theme.surface50);
  root.style.setProperty('--color-surface-100', theme.surface100);
  root.style.setProperty('--color-surface-200', theme.surface200);
  root.style.setProperty('--color-surface-300', theme.surface300);
  root.style.setProperty('--color-surface-400', theme.surface400);
  root.style.setProperty('--color-surface-500', theme.surface500);
  root.style.setProperty('--color-border', theme.border);
  root.style.setProperty('--color-border-subtle', theme.borderSubtle);
  root.style.setProperty('--color-border-emphasis', theme.borderEmphasis);
  root.style.setProperty('--color-text-strong', theme.textStrong);
  root.style.setProperty('--color-text-base', theme.textBase);
  root.style.setProperty('--color-text-muted', theme.textMuted);
  root.style.setProperty('--color-text-subtle', theme.textSubtle);
  root.style.setProperty('--theme-scrollbar', theme.scrollbar);
  root.style.setProperty('--theme-scrollbar-hover', theme.scrollbarHover);
  root.setAttribute('data-theme', theme.id);
  root.setAttribute('data-scheme', theme.scheme);
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    return themes.find(t => t.id === saved) || themes[0];
  });

  useLayoutEffect(() => {
    applyTheme(theme);
  }, [theme]);

  const setTheme = useCallback((t: Theme) => {
    setThemeState(t);
    localStorage.setItem(STORAGE_KEY, t.id);
  }, []);

  return { theme, setTheme, themes };
}
