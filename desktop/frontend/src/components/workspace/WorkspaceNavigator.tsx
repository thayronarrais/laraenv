import { PanelLeftClose } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useT } from '../../lib/i18n';
import { clampSidebarWidth, type NavigationPreferences, type NavigationPreferencesPatch } from './navigationPreferences';
import type { NavigatorTab } from './types';
export type WorkspaceNavigatorProps = { preferences: NavigationPreferences; active: boolean; compact: boolean; onUpdate(patch: NavigationPreferencesPatch): void; onClaimNavigator(): void; panels: Record<NavigatorTab, ReactNode> };
const tabs: NavigatorTab[] = ['agents', 'files', 'git'];
const labels = { agents: 'workspace.navigation.agents', files: 'workspace.navigation.files', git: 'workspace.navigation.git' };
export function WorkspaceNavigator({ preferences, active, compact, onUpdate, onClaimNavigator, panels }: WorkspaceNavigatorProps) {
  const t = useT(); const host = useRef<HTMLDivElement>(null); const [available, setAvailable] = useState(0);
  const drag = useRef<{ id: number; x: number; width: number } | null>(null);
  useEffect(() => {
    // The drawer's host occupies zero width. Measuring its parent therefore
    // never subtracts the drawer itself and cannot oscillate as it opens.
    const area = host.current?.parentElement; if (!area) return;
    const measure = () => setAvailable(area.getBoundingClientRect().width); measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure); observer.observe(area); return () => observer.disconnect();
  }, []);
  const visible = active && preferences.sidebarVisible;
  const width = Math.min(clampSidebarWidth(preferences.sidebarWidth), available > 0 ? (compact ? available * .8 : available) : Infinity);
  useEffect(() => { if (!visible) drag.current = null; }, [visible]);
  return <div ref={host} className="relative h-full shrink-0" style={{ width: compact || !visible ? 0 : width }}>
    <aside id="workspace-navigator" aria-label={t('workspace.navigation.title')} className="absolute inset-y-0 left-0 z-30 flex min-h-0 flex-col border-r border-white/10 bg-[#111315]" style={{ width, display: visible ? undefined : 'none' }} onPointerDownCapture={onClaimNavigator} onFocusCapture={onClaimNavigator}>
      <div className="flex shrink-0 items-center border-b border-white/10">
        <div role="tablist" aria-label={t('workspace.navigation.title')} className="flex min-w-0 flex-1">
          {tabs.map(tab => <button key={tab} type="button" role="tab" aria-selected={preferences.sidebarTab === tab} aria-controls={`workspace-panel-${tab}`} id={`workspace-tab-${tab}`} className={`flex-1 px-2 py-3 text-xs ${preferences.sidebarTab === tab ? 'bg-white/10 text-white' : 'text-white/60'}`} onClick={() => onUpdate({ sidebarTab: tab, sidebarVisible: true })}>{t(labels[tab])}</button>)}
        </div>
        <button type="button" aria-label={t('workspace.navigation.hide')} title={t('workspace.navigation.hide')} aria-controls="workspace-navigator" aria-expanded={visible} className="workspace-icon-button mx-1" onClick={() => onUpdate({ sidebarVisible: false })}><PanelLeftClose aria-hidden="true" className="h-4 w-4"/></button>
      </div>
      {tabs.map(tab => { const panelActive = visible && preferences.sidebarTab === tab; return <div key={tab} role="tabpanel" id={`workspace-panel-${tab}`} aria-labelledby={`workspace-tab-${tab}`} data-active={panelActive} hidden={!panelActive} className="min-h-0 flex-1">{panels[tab]}</div>; })}
      <div role="separator" aria-label={t('workspace.navigation.resize')} aria-orientation="vertical" aria-valuemin={240} aria-valuemax={420} aria-valuenow={preferences.sidebarWidth} tabIndex={0} className="absolute inset-y-0 -right-1 w-2 cursor-col-resize touch-none focus-visible:bg-accent/50"
        onPointerDown={event => { if (event.button !== 0) return; drag.current = { id: event.pointerId, x: event.clientX, width }; event.currentTarget.setPointerCapture(event.pointerId); }}
        onPointerMove={event => { const start = drag.current; if (start?.id === event.pointerId) onUpdate({ sidebarWidth: clampSidebarWidth(start.width + event.clientX - start.x) }); }}
        onPointerUp={event => { if (drag.current?.id === event.pointerId) drag.current = null; }} onLostPointerCapture={() => { drag.current = null; }}
        onKeyDown={event => { const next = event.key === 'ArrowLeft' ? preferences.sidebarWidth - 10 : event.key === 'ArrowRight' ? preferences.sidebarWidth + 10 : event.key === 'Home' ? 240 : event.key === 'End' ? 420 : null; if (next !== null) { event.preventDefault(); onUpdate({ sidebarWidth: clampSidebarWidth(next) }); } }} />
    </aside>
  </div>;
}
