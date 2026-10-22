import type { NavigatorTab } from './types';
export type NavigationPreferences = { version: 1; primaryCollapsed: boolean; sidebarVisible: boolean; sidebarTab: NavigatorTab; sidebarWidth: number };
export type NavigationPreferencesPatch = Partial<Omit<NavigationPreferences, 'version'>>;
export const navigationStorageKey = 'laraenv.workspaceNavigation';
export const defaultNavigationPreferences: NavigationPreferences = { version: 1, primaryCollapsed: false, sidebarVisible: true, sidebarTab: 'agents', sidebarWidth: 300 };
export function clampSidebarWidth(width: number): number { return Math.min(420, Math.max(240, width)); }
export function readNavigationPreferences(): NavigationPreferences {
  try {
    const raw = localStorage.getItem(navigationStorageKey);
    if (raw === null) return { ...defaultNavigationPreferences, sidebarVisible: localStorage.getItem('laraenv.agentNavigatorVisible') !== 'false' };
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return { ...defaultNavigationPreferences };
    const candidate = value as Record<string, unknown>;
    if (candidate.version !== 1 || typeof candidate.primaryCollapsed !== 'boolean' || typeof candidate.sidebarVisible !== 'boolean' ||
      typeof candidate.sidebarTab !== 'string' || !['agents', 'files', 'git'].includes(candidate.sidebarTab) || typeof candidate.sidebarWidth !== 'number' || !Number.isFinite(candidate.sidebarWidth)) return { ...defaultNavigationPreferences };
    return { version: 1, primaryCollapsed: candidate.primaryCollapsed, sidebarVisible: candidate.sidebarVisible, sidebarTab: candidate.sidebarTab as NavigatorTab, sidebarWidth: clampSidebarWidth(candidate.sidebarWidth) };
  } catch { return { ...defaultNavigationPreferences }; }
}
export function updateNavigationPreferences(current: NavigationPreferences, patch: NavigationPreferencesPatch): NavigationPreferences {
  return { ...current, ...patch, version: 1, sidebarWidth: Number.isFinite(patch.sidebarWidth) ? clampSidebarWidth(patch.sidebarWidth!) : current.sidebarWidth };
}
export function writeNavigationPreferences(preferences: NavigationPreferences): void {
  try { localStorage.setItem(navigationStorageKey, JSON.stringify(preferences)); } catch { /* Storage is best effort. */ }
}
