// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useWorkspaceNavigationPreferences } from '../useWorkspaceNavigationPreferences';
beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); vi.restoreAllMocks(); });
it('migrates visibility without losing preferred width on subsequent opens', () => {
  localStorage.setItem('laraenv.agentNavigatorVisible', 'false');
  const first = renderHook(useWorkspaceNavigationPreferences);
  expect(first.result.current.preferences).toEqual({ version: 1, primaryCollapsed: false, sidebarVisible: false, sidebarTab: 'agents', sidebarWidth: 300 });
  act(() => first.result.current.update({ sidebarWidth: 390, sidebarTab: 'files', primaryCollapsed: true }));
  first.unmount(); localStorage.setItem('laraenv.agentNavigatorVisible', 'true');
  const next = renderHook(useWorkspaceNavigationPreferences);
  expect(next.result.current.preferences).toEqual({ version: 1, primaryCollapsed: true, sidebarVisible: false, sidebarTab: 'files', sidebarWidth: 390 });
});
it.each(['{', '{"version":2}', '{"version":1,"sidebarWidth":"300"}', 'null'])('malformed preferences %s fall back to first-open defaults', raw => {
  localStorage.setItem('laraenv.workspaceNavigation', raw); localStorage.setItem('laraenv.agentNavigatorVisible', 'false');
  expect(renderHook(useWorkspaceNavigationPreferences).result.current.preferences).toEqual({ version: 1, primaryCollapsed: false, sidebarVisible: true, sidebarTab: 'agents', sidebarWidth: 300 });
});
it('rejects an array-valued tab even when it coerces to a valid tab name', () => {
  localStorage.setItem('laraenv.workspaceNavigation', JSON.stringify({ version: 1, primaryCollapsed: true, sidebarVisible: false, sidebarTab: ['files'], sidebarWidth: 390 }));
  const hook = renderHook(useWorkspaceNavigationPreferences);
  expect(hook.result.current.preferences).toEqual({ version: 1, primaryCollapsed: false, sidebarVisible: true, sidebarTab: 'agents', sidebarWidth: 300 });
});
it('limits preferred width to 240–420 and keeps updates usable when storage fails', () => {
  const hook = renderHook(useWorkspaceNavigationPreferences);
  act(() => hook.result.current.update({ sidebarWidth: 1 })); expect(hook.result.current.preferences.sidebarWidth).toBe(240);
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('full'); });
  act(() => hook.result.current.update({ sidebarWidth: 500, sidebarVisible: false }));
  expect(hook.result.current.preferences.sidebarWidth).toBe(420); expect(hook.result.current.preferences.sidebarVisible).toBe(false);
});
