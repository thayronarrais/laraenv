// @vitest-environment jsdom
import { act, useEffect, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
const mock = vi.hoisted(() => ({ mounts: 0, unmounts: 0, documentMounts: 0, documentUnmounts: 0, terminalProps: {} as any, filesProps: {} as any, documentProps: {} as any, controller: null as any, auto: {} as any, events: new Map<string, (value:any) => void>(), load: vi.fn(), saveLayout: vi.fn(), detached: false, bar: false, resize: null as any, paletteHotkey: "" }));
vi.mock('../../../../wailsjs/go/main/App', () => ({
 CloudStatus: async () => ({}), GetAgentSessionsEnabled: async () => false, SetAgentSessionsEnabled: vi.fn(), GetDetachedSeed: async () => '', IsDetachedWindow: async () => mock.detached, IsCommandBar: async () => mock.bar, SendCommandIntent: vi.fn(), QuitApp: vi.fn(),
 SetDocumentWorkspaceState: async () => {}, ResolveDocumentShutdown: async () => {},
}));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: (key:string, cb:any) => { mock.events.set(key, cb); return () => mock.events.delete(key); }, WindowToggleMaximise: vi.fn() }));
vi.mock('../../../lib/i18n', () => { const t = (key:string) => key; return {useT: () => t}; });
vi.mock('../../../commandCenter/useCommandCenter', () => ({useCommandCenter: () => ({commands:[], customCommands:[], paletteHotkey:mock.paletteHotkey, projects:[], sshHosts:[], agentProviders:[], reload:vi.fn()})}));
vi.mock('../../agents/useAgentAutoSwitch', () => ({useAgentAutoSwitch: (props:any) => {mock.auto = props;return {waiting:0};}}));
vi.mock('../../Sidebar', () => ({default: ({onChange}:any) => <nav><button onClick={() => onChange('terminal')}>Terminal page</button><button onClick={() => onChange('dashboard')}>Dashboard page</button></nav>}));
vi.mock('../../terminal/TerminalWorkspace', () => ({default: function MockTerminalWorkspace(props:any) {
 mock.terminalProps = props;
 const [alive,setAlive] = useState(true);
 useEffect(() => {mock.mounts++; return () => {mock.unmounts++;};}, []);
 return <div data-testid="terminal-mock">{alive && <button onClick={() => setAlive(false)}>Last PTY exits</button>}<button onClick={() => props.onActiveSurfaceChange?.('terminal')}>Terminal pointer</button></div>;
}}));
vi.mock('../api', () => ({workspaceDocumentApi: {
 loadLayout: (...a:any[]) => mock.load(...a), saveLayout: (...a:any[]) => mock.saveLayout(...a),
 read: async () => ({code:'ok',value:{canonicalPath:'C:/demo/readme.md',text:'saved',revision:'rev',size:5,bom:false,eol:'lf',readOnly:false}}), check: async () => ({code:'unchanged'}),
}}));
vi.mock('../DocumentWorkspace', () => ({DocumentWorkspace: (props:any) => {
 const {controller,onClaimDocuments} = props; mock.documentProps = props;
 mock.controller = controller;
 useEffect(() => {mock.documentMounts++;return () => {mock.documentUnmounts++;};}, []);
 return <div data-testid="documents-mock" onFocusCapture={onClaimDocuments} onPointerDownCapture={onClaimDocuments}>
 <input aria-label="source input" />
 <button onClick={() => controller.edit(controller.buffers[0].id,'unsaved')}>Edit buffer</button><button>Preview button</button>
 <button onClick={() => controller.closeTab(controller.activeTabId)}>Close document</button><span>{controller.buffers[0]?.text}</span></div>;
}}));
vi.mock('../../WindowControls', () => ({default: () => null}));
vi.mock('../../../pages/Dashboard', () => ({default: () => null}));
vi.mock('../../../pages/Downloads', () => ({default: () => null}));
vi.mock('../../../pages/Projects', () => ({default: () => null}));
vi.mock('../../../pages/SSH', () => ({default: () => null}));
vi.mock('../../../pages/Settings', () => ({default: () => null}));
vi.mock('../../../pages/Cron', () => ({default: () => null}));
vi.mock('../../../pages/Deployments', () => ({default: () => null}));
vi.mock('../../../pages/Agents', () => ({default: () => null}));
vi.mock('../../ServiceLogsModal', () => ({default: () => null}));
vi.mock('../../PortsModal', () => ({default: () => null}));
vi.mock('../../CommandPalette', () => ({default: () => null}));
vi.mock('../../agents/AgentUsageBar', () => ({default: () => null}));
vi.mock('../../NotificationBell', () => ({default: () => null}));

vi.mock('../WorkspaceFilesPanel', () => ({WorkspaceFilesPanel: (props:any) => {mock.filesProps=props; return null;}}));
vi.mock('../../sourcecontrol/api', () => ({sourceControlApi:{subscribeOperations:()=>()=>{}}}));
import App from '../../../App';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let host:HTMLDivElement, root:Root;
const click = async (text:string) => { const button = [...document.querySelectorAll('button')].find(b => b.textContent === text); expect(button, text).toBeTruthy(); await act(async () => {button!.click();}); };
const measure = async (width:number) => {await act(async () => {mock.resize?.([{contentRect:{width}}]);});};
beforeEach(async () => {
 Object.assign(mock,{mounts:0,unmounts:0,documentMounts:0,documentUnmounts:0,detached:false,bar:false,paletteHotkey:""}); mock.events.clear();
 mock.load.mockReset().mockResolvedValue(JSON.stringify({version:1,tabs:[{references:[{context:{kind:'project',projectName:'demo'},rootKey:'a'.repeat(64),relativePath:'readme.md'}],mode:'preview'}],activeTabIndex:0,visible:true,panelPercent:45})); mock.saveLayout.mockResolvedValue(undefined);
 (globalThis as any).ResizeObserver = class {constructor(callback:any){mock.resize=callback;} observe(){} disconnect(){}};
 vi.spyOn(document,'hasFocus').mockReturnValue(true);
 host=document.createElement('div');document.body.appendChild(host);root=createRoot(host);
});
afterEach(async () => {await act(async () => root.unmount());host.remove();vi.restoreAllMocks();});
const mount = async () => {await act(async () => {root.render(<App/>);});};
it('retains documents and PTYs when pages or terminal groups close', async () => {
 await mount(); await click('Terminal page'); await click('Edit buffer');
 await click('Dashboard page'); await click('Terminal page');
 await click('workspace.navigation.agents'); await click('workspace.navigation.agents');
 expect(mock.mounts).toBe(1);expect(mock.unmounts).toBe(0);
 expect(mock.documentMounts).toBe(1);expect(mock.documentUnmounts).toBe(0);
 expect(mock.controller.buffers[0].text).toBe('unsaved');
 await click('Last PTY exits');expect(mock.controller.buffers[0].text).toBe('unsaved');
 expect(mock.load).toHaveBeenCalledTimes(1);expect(mock.events.has('documents:shutdown-request')).toBe(true);
});
it('preview and dialogs retain document ownership even on another page', async () => {
 await mount();await click('Terminal page');
 await act(async () => {[...host.querySelectorAll('button')].find(b => b.textContent === 'Preview button')!.focus();});
 expect(mock.terminalProps.activeSurface).toBe('documents');expect(mock.auto.blocked).toBe(true);
 await click('Edit buffer');await click('Dashboard page');
 await act(async () => {mock.events.get('documents:shutdown-request')?.({requestId:'quit-1'});});
 expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
 expect(document.activeElement?.textContent).toBe('documents.close.cancel');
 expect(mock.terminalProps.activeSurface).toBe('documents');
 await click('documents.close.cancel');expect(mock.controller.buffers[0].text).toBe('unsaved');
});
it('uses available width at 719/720 with navigator open and keeps both trees mounted', async () => {
 await mount();await click('Terminal page');await measure(719);
 const terminal = host.querySelector<HTMLElement>('[data-testid="terminal-surface"]');
 const documents = host.querySelector<HTMLElement>('[data-testid="document-surface"]');
 expect(terminal).not.toBeNull();expect(documents).not.toBeNull();
 await act(async () => { host.querySelector<HTMLButtonElement>('[data-testid="workspace-surfaces"] [role="tab"]:nth-child(2)')!.click(); });expect(terminal!.style.display).toBe('none');expect(documents!.style.display).not.toBe('none');
 await click('terminal.app.terminalLabel');expect(documents!.style.display).toBe('none');
 await measure(720);expect(documents!.style.display).not.toBe('none');expect(terminal!.style.display).not.toBe('none');
 expect(documents!.style.width).toBe('400px');expect(mock.mounts).toBe(1);expect(mock.documentMounts).toBe(1);
});
it.each(['detached','bar'] as const)('does not mount documents in %s windows',async mode => {
 mock[mode]=true;await mount();expect(mock.documentMounts).toBe(0);expect(mock.load).not.toHaveBeenCalled();expect(mock.events.has('documents:shutdown-request')).toBe(false);
});

it('explicit terminal selection releases document input focus', async () => {
 await mount();await click('Terminal page');
 const source = host.querySelector<HTMLInputElement>('[aria-label="source input"]')!;
 await act(async () => {source.focus();});
 expect(mock.terminalProps.activeSurface).toBe('documents');
 await act(async () => {mock.terminalProps.onActiveSurfaceChange('terminal');});
 expect(document.activeElement).not.toBe(source);expect(mock.terminalProps.activeSurface).toBe('terminal');
});

it('reserves Ctrl+S for documents even when the palette uses that shortcut', async () => {
 mock.paletteHotkey='Ctrl+S';await mount();await click('Terminal page');
 const source = host.querySelector<HTMLInputElement>('[aria-label="source input"]')!;
 await act(async () => {source.focus();});
 const save = new KeyboardEvent('keydown',{key:'s',ctrlKey:true,bubbles:true,cancelable:true});
 await act(async () => {source.dispatchEvent(save);});
 expect(save.defaultPrevented).toBe(false);
});
it('keeps manual project fallback separate from actual terminal selection and denies it to SSH', async () => {
 await mount(); await click('Terminal page');
 await act(async () => {mock.filesProps.onSelectProject('manual');});
 expect(mock.documentProps.context).toEqual({kind:'project',projectName:'manual'});
 await act(async () => {mock.terminalProps.onWorkspaceSelectionChange?.({kind:'local',context:{kind:'agent',sessionId:'worktree-session'}});});
 expect(mock.documentProps.context).toEqual({kind:'agent',sessionId:'worktree-session'});
 expect(mock.terminalProps.activeSurface).toBe('navigator');
 await act(async () => {mock.terminalProps.onWorkspaceSelectionChange?.({kind:'ssh',context:null});});
 expect(mock.documentProps.context).toBeNull();
 await act(async () => {mock.terminalProps.onWorkspaceSelectionChange?.({kind:'local',context:null});});
 expect(mock.documentProps.context).toEqual({kind:'project',projectName:'manual'});
 await act(async () => {mock.terminalProps.onOpenWorkspaceFiles(null);});
 expect(mock.documentProps.context).toEqual({kind:'project',projectName:'manual'});
});
