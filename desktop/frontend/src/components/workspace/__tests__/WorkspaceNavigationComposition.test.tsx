// @vitest-environment jsdom
import { act, useEffect, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
const mock = vi.hoisted(() => ({ enabled: false, realAuto: false, interests: vi.fn(), removeInterest: vi.fn(), getActivity: vi.fn(), agentsProps: {} as any, listeners: new Map<string, Set<(value:any)=>void>>(), mounts: 0, unmounts: 0, documentMounts: 0, documentUnmounts: 0, terminalProps: {} as any, documentProps: {} as any, controller: null as any, auto: {} as any, events: new Map<string, (value:any) => void>(), load: vi.fn(), saveLayout: vi.fn(), detached: false, bar: false, resize: null as any, paletteHotkey: "", customRun: vi.fn(), gitResolve: vi.fn(), gitStage: vi.fn(), gitCommit: vi.fn(), gitDiff: vi.fn(), fileContext: vi.fn(), fileRead: vi.fn(), commandIntent: null as any, observers: new Map<Element, any>(), widths: new Map<Element, number>() }));
vi.mock('../../../../wailsjs/go/main/App', () => ({
 CloudStatus: async () => ({}), GetAgentSessionsEnabled: async () => mock.enabled, SetAgentSessionsEnabled: vi.fn(), GetDetachedSeed: async () => '', IsDetachedWindow: async () => mock.detached, IsCommandBar: async () => mock.bar, SendCommandIntent: vi.fn(), QuitApp: vi.fn(), ListProjects: async () => [{name:"manual"}], AppVersion: async () => "0.5.9", OpenURL: vi.fn(),
 GetAgentActivity: (...a:any[]) => mock.getActivity(...a), RefreshAgentActivity: (...a:any[]) => mock.getActivity(...a), SetAgentActivityInterest: (...a:any[]) => mock.interests(...a), RemoveAgentActivityInterest: (...a:any[]) => mock.removeInterest(...a), ListAgentSessions: async () => [],
 SetDocumentWorkspaceState: async () => {}, ResolveDocumentShutdown: async () => {},
}));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: (key:string, cb:any) => { const listeners = mock.listeners.get(key) ?? new Set(); listeners.add(cb); mock.listeners.set(key,listeners); mock.events.set(key, (value:any) => {for(const listener of [...listeners]) listener(value);}); return () => listeners.delete(cb); }, WindowToggleMaximise: vi.fn() }));
vi.mock('../../../lib/i18n', () => { const t = (key:string) => key; return {useT: () => t, useI18n: () => ({t, formatNumber:String, formatDate: (date:Date) => date.toISOString()})}; });
vi.mock('../../../commandCenter/useCommandCenter', () => ({useCommandCenter: () => ({commands:[], customCommands:[{hotkey:"x",run:mock.customRun}], paletteHotkey:mock.paletteHotkey, runIntent:mock.customRun, projects:[], sshHosts:[], agentProviders:[], reload:vi.fn()})}));
vi.mock('../../agents/useAgentAutoSwitch', async (original) => {const real:any=await original(); return {useAgentAutoSwitch: (props:any) => {mock.auto = props;return mock.realAuto ? real.useAgentAutoSwitch(props) : {waiting:0};}};});
vi.mock('../../terminal/TerminalWorkspace', () => ({default: function TerminalMock(props:any) {
 mock.terminalProps = props;
 const [alive,setAlive] = useState(true);
 useEffect(()=>{if(props.focusAgentRequest && (!props.activeSurface || props.activeSurface==='terminal')) document.querySelector<HTMLTextAreaElement>('[data-testid="terminal-mock"] textarea')?.focus();},[props.focusAgentRequest,props.activeSurface]);
 useEffect(() => {mock.mounts++; return () => {mock.unmounts++;};}, []);
 return <div data-testid="terminal-mock"><div className="xterm"><textarea aria-label="PTY input"/></div>{alive && <button onClick={() => setAlive(false)}>Last PTY exits</button>}<button onClick={() => props.onActiveSurfaceChange?.('terminal')}>Terminal pointer</button></div>;
}}));
vi.mock('../../documents/api', () => ({workspaceDocumentApi: {
 loadLayout: (...a:any[]) => mock.load(...a), saveLayout: (...a:any[]) => mock.saveLayout(...a),
 context: (...a:any[]) => mock.fileContext(...a), list: async () => ({code:'ok',value:{entries:[{name:'readme.md',relativePath:'readme.md',kind:'file'}],nextCursor:''}}), read: (...a:any[]) => mock.fileRead(...a), check: async () => ({code:'unchanged'}),
}}));
vi.mock('../../documents/DocumentWorkspace', () => ({DocumentWorkspace: (props:any) => {
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
vi.mock('../../../pages/Agents', () => ({default: (props:any) => {mock.agentsProps=props;return <><input aria-label='agent filter' data-active={props.active}/><button onClick={()=>props.onOpenMap?.({id:'menu-root'})}>Map menu root</button></>;}}));
vi.mock('../../ServiceLogsModal', () => ({default: () => null}));
vi.mock('../../PortsModal', () => ({default: () => null}));
vi.mock('../../CommandPalette', () => ({default: () => null}));
vi.mock('../../agents/AgentUsageBar', () => ({default: ({leading}:any) => <div data-testid='usage-bar'>{leading}</div>}));
vi.mock('../../NotificationBell', () => ({default: () => null}));

vi.mock('../../sourcecontrol/api', () => ({ sourceControlApi: {
 resolve: (...a:any[]) => mock.gitResolve(...a), status: async (ref:any) => ({code:'ok',value:{...snapshot(),repository:ref,entries:[entry('one','readme.md')]}}),
 stage: (...a:any[]) => mock.gitStage(...a), commit: (...a:any[]) => mock.gitCommit(...a), diff: (...a:any[]) => mock.gitDiff(...a), subscribeOperations: () => () => {},
}, problemCode: (code:string) => code }));
import { snapshot, entry, diff } from '../../sourcecontrol/__tests__/uiFixtures';
import App from '../../../App';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let host:HTMLDivElement, root:Root;
const click = async (text:string) => { const button = [...document.querySelectorAll('button')].find(b => b.textContent?.trim() === text || b.getAttribute('aria-label') === text); expect(button, text).toBeTruthy(); await act(async () => {button!.click();}); };
const measure = async (width:number, id='workspace-surfaces') => { const el=host.querySelector(`[data-testid="${id}"]`)!; expect(el).toBeTruthy(); mock.widths.set(el,width); await act(async () => {for (const cb of mock.observers.get(el) ?? []) cb([{target:el,contentRect:{width}}]);});};
beforeEach(async () => {
 Object.assign(mock,{mounts:0,unmounts:0,documentMounts:0,documentUnmounts:0,enabled:false,realAuto:false,detached:false,bar:false,paletteHotkey:""}); mock.events.clear(); mock.listeners.clear(); mock.interests.mockReset().mockResolvedValue(undefined); mock.removeInterest.mockReset().mockResolvedValue(undefined); mock.getActivity.mockReset().mockImplementation(async(id:string)=>activity(id)); const storage = new Map<string,string>(); vi.stubGlobal("localStorage", {getItem:(key:string)=>storage.get(key) ?? null,setItem:(key:string,value:string)=>storage.set(key,value)}); mock.observers.clear(); mock.widths.clear(); mock.auto={}; mock.customRun.mockClear(); mock.gitStage.mockReset(); mock.gitCommit.mockReset(); mock.fileContext.mockReset().mockImplementation(async (context:any) => ({code:'ok',value:{...snapshot().root,label:context.projectName ?? context.sessionId}})); mock.fileRead.mockReset().mockResolvedValue({code:'ok',value:{canonicalPath:'C:/demo/readme.md',text:'saved',revision:'rev',size:5,bom:false,eol:'lf',readOnly:false}}); mock.gitResolve.mockReset().mockImplementation(async (context:any) => ({code:'ok',value:{...snapshot(),repository:{...snapshot().repository,context},entries:[entry('one','readme.md')]}})); mock.gitDiff.mockReset().mockResolvedValue({code:'ok',value:diff()});
 mock.load.mockReset().mockResolvedValue(JSON.stringify({version:1,tabs:[{references:[{context:{kind:'project',projectName:'demo'},rootKey:'a'.repeat(64),relativePath:'readme.md'}],mode:'preview'}],activeTabIndex:0,visible:true,panelPercent:45})); mock.saveLayout.mockResolvedValue(undefined);
 (globalThis as any).ResizeObserver = class { constructor(private cb:any){} observe(el:Element){mock.observers.set(el,[...(mock.observers.get(el) ?? []),this.cb]);} disconnect(){} }; vi.spyOn(Element.prototype,"getBoundingClientRect").mockImplementation(function(this:Element){return {width:mock.widths.get(this) ?? 1200, right:1200} as DOMRect;});
 vi.spyOn(document,'hasFocus').mockReturnValue(true);
 host=document.createElement('div');document.body.appendChild(host);root=createRoot(host);
});
afterEach(async () => {await act(async () => root.unmount());host.remove();vi.restoreAllMocks();vi.useRealTimers();});
const mount = async () => {await act(async () => {root.render(<App/>);});};

const go = async () => {await mount();await click('sidebar.nav.terminal');};
const nav = () => host.querySelector<HTMLElement>('aside[aria-label="workspace.navigation.title"]')!;
const toolbar = async (tab:string) => { if(nav().style.display === 'none') await click('workspace.navigation.show'); const button=host.querySelector<HTMLButtonElement>(`#workspace-tab-${tab}`)!; expect(button).toBeTruthy(); await act(async()=>button.click()); };
it('one navigator preserves mounted panel inputs, PTYs and dirty documents through tabs, hide and pages',async () => {
 await go(); const agents=host.querySelector<HTMLInputElement>('[aria-label="agent filter"]')!; agents.value='kept';
 await click('Edit buffer'); await toolbar('files'); const files=host.querySelector('#workspace-panel-files'); await toolbar('git'); const git=host.querySelector('#workspace-panel-git');
 await click('workspace.navigation.hide');await click('sidebar.nav.dashboard');await click('sidebar.nav.terminal');await toolbar('agents');
 expect(host.querySelector('[aria-label="agent filter"]')).toBe(agents);expect(agents.value).toBe('kept');expect(host.querySelector('#workspace-panel-files')).toBe(files);expect(host.querySelector('#workspace-panel-git')).toBe(git);
 expect(mock.mounts).toBe(1);expect(mock.documentMounts).toBe(1);expect(mock.controller.buffers[0].text).toBe('unsaved');
 await click('sidebar.collapse');expect(host.querySelector('aside')!.style.width).toBe('56px');expect(host.querySelector('button[aria-label="sidebar.nav.settings"]')).not.toBeNull();
});
it('files and Git follow selected leaves while a manual fallback cannot override agent or SSH and documents retain origin',async () => {
 await go();await toolbar('files');const picker=host.querySelector('select')!;
 await act(async () => {picker.value='manual';picker.dispatchEvent(new Event('change',{bubbles:true}));});
 expect(mock.fileContext).toHaveBeenLastCalledWith({kind:'project',projectName:'manual'});
 await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'agent',sessionId:'worktree'}}));
 expect(mock.fileContext).toHaveBeenLastCalledWith({kind:'agent',sessionId:'worktree'});await toolbar('git');expect(mock.gitResolve).toHaveBeenLastCalledWith({kind:'agent',sessionId:'worktree'});
 expect(mock.controller.buffers[0].references[0].context).toEqual({kind:'project',projectName:'demo'});
 await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'ssh',context:null}));await toolbar('files');expect(host.textContent).toContain('workspace.navigation.sshFiles');expect(mock.terminalProps.openFilesRequest?.seq).toBeGreaterThan(0);
 expect(host.querySelector('#workspace-panel-files select')).toBeNull();
});
it('navigator typing owns shortcuts and explicit terminal request releases navigator input',async () => {
 await go();await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'project',projectName:'demo'}}));await toolbar('git');
 const field=host.querySelector<HTMLTextAreaElement>('#workspace-panel-git textarea')!;await act(async () => field.focus());expect(mock.auto.blocked).toBe(true);expect(mock.terminalProps.activeSurface).toBe('navigator');
 await act(async () => field.dispatchEvent(new KeyboardEvent('keydown',{key:'x',bubbles:true,cancelable:true})));expect(mock.customRun).not.toHaveBeenCalled();
 await act(async () => mock.terminalProps.onActiveSurfaceChange('terminal'));expect(document.activeElement).not.toBe(field);expect(mock.terminalProps.activeSurface).toBe('terminal');
});
it('stable dock/drawer sizing uses outer area and retains preferred widths while actual split changes at719/720',async () => {
 await go();await measure(1138,'workspace-navigation-area');await measure(838);expect(nav().parentElement!.style.width).toBe('300px');
 await measure(772,'workspace-navigation-area');await measure(772);expect(nav().parentElement!.style.width).toBe('0px');expect(nav().style.width).toBe('300px');
 const docs=host.querySelector<HTMLElement>('[data-testid="document-surface"]')!;expect(docs.style.width).toContain('400px');expect(mock.controller.panelPercent).toBe(45);
 await click('workspace.navigation.hide');await measure(772,'workspace-navigation-area');await toolbar('files');expect(nav().parentElement!.style.width).toBe('0px');
 await measure(719);expect(host.querySelector('[data-testid="workspace-surfaces"] [role="tablist"]')).not.toBeNull();await measure(720);expect(host.querySelector('[data-testid="workspace-surfaces"] [role="tablist"]')).toBeNull();
 expect(JSON.parse(localStorage.getItem('laraenv.workspaceNavigation')!).sidebarWidth).toBe(300);
});
it.each(['file','diff'])('opening a %s reveals editor and closes narrow drawer without retargeting document model',async kind => {
 await go();await measure(772,'workspace-navigation-area');await measure(772);await click('workspace.navigation.hideEditor');
 await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'project',projectName:'demo'}}));
 await toolbar(kind==='file'?'files':'git');expect(mock.controller.visible).toBe(false);
 if(kind==='file') await click('readme.md'); else await act(async () => host.querySelector<HTMLButtonElement>('#workspace-panel-git button[aria-label="git.review readme.md"]')!.click());
 expect(mock.controller.visible).toBe(true);expect(nav().style.display).toBe('none');expect(mock.terminalProps.activeSurface).toBe('documents');
});
it('global staging decision survives navigation and blocks terminal claims, custom commands and competing shutdown',async () => {
 await go();await click('Edit buffer');await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'project',projectName:'demo'}}));await toolbar('git');await click('git.stageFile readme.md');
 const dialog=document.querySelector('[role="dialog"]')!;expect(dialog).not.toBeNull();expect(mock.auto.blocked).toBe(true);
 await click('sidebar.nav.dashboard');await act(async () => mock.terminalProps.onActiveSurfaceChange('terminal'));expect(mock.terminalProps.activeSurface).not.toBe('terminal');
 await act(async () => {document.dispatchEvent(new KeyboardEvent('keydown',{key:'x',bubbles:true,cancelable:true}));mock.events.get('documents:shutdown-request')?.({requestId:'stage-quit'});});
 expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);expect(document.querySelector('[role="dialog"]')).toBe(dialog);expect(mock.customRun).not.toHaveBeenCalled();
 const buttons=dialog.querySelectorAll('button');await act(async () => {buttons[buttons.length-1].focus();buttons[buttons.length-1].dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true}));});expect(document.activeElement).toBe(buttons[0]);
 await click('git.choice.cancel');expect(mock.gitStage).not.toHaveBeenCalled();expect(mock.controller.buffers[0].text).toBe('unsaved');
});

it('opening a file clears a selected Git review and reveals the same document controller',async () => {
 await go();await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'project',projectName:'demo'}}));await toolbar('git');await click('git.review readme.md');
 expect(mock.documentProps.reviews.activeTabId).not.toBeNull();await toolbar('files');await click('readme.md');expect(mock.documentProps.reviews.activeTabId).toBeNull();expect(mock.controller.activeTabId).not.toBeNull();expect(mock.documentMounts).toBe(1);
});

it('global stage choice prevents externally dispatched command intents',async () => {
 await go();await click('Edit buffer');await act(async () => mock.terminalProps.onWorkspaceSelectionChange({kind:'local',context:{kind:'project',projectName:'demo'}}));await toolbar('git');await click('git.stageFile readme.md');
 await act(async () => mock.events.get('commandcenter:intent')?.(JSON.stringify({id:'terminal.new'})));expect(mock.customRun).not.toHaveBeenCalled();await click('git.choice.cancel');
});

it('exposes one visible tab set and reopens the selected tab through a visible controlled button',async()=> {
 await go();
 const visible=()=>[...host.querySelectorAll('button')].filter(b=>!b.closest('[hidden]') && !b.closest('[style*="display: none"]'));
 for(const tab of ['agents','files','git']) expect(visible().filter(b=>b.textContent?.trim()===`workspace.navigation.${tab}` || b.getAttribute('aria-label')===`workspace.navigation.${tab}`)).toHaveLength(1);
 await toolbar('git'); await click('workspace.navigation.hide');
 const reopen=visible().find(b=>b.getAttribute('aria-label')==='workspace.navigation.show'); expect(reopen).toBeTruthy(); expect(reopen!.getAttribute('aria-controls')).toBe('workspace-navigator'); expect(reopen!.getAttribute('aria-expanded')).toBe('false');
 await act(async()=>reopen!.click()); expect(nav().style.display).not.toBe('none'); expect(host.querySelector('#workspace-tab-git')!.getAttribute('aria-selected')).toBe('true'); expect(mock.mounts).toBe(1);expect(mock.documentMounts).toBe(1);
});

function activity(sessionId:string, revision=1) {
 return {sessionId,runId:'run',sourceId:'owner',revision,rootId:'root',provider:'codex',observedSince:'2026-10-15T12:00:00Z',observedAt:'2026-10-15T12:00:00Z',status:'available',messageCode:'none',capabilities:{children:true,backgroundTasks:false,liveState:false,duration:false,model:false,tokens:false},nodes:[{id:'root',providerNodeId:'root',kind:'agent',label:sessionId,state:'unknown',terminalId:`term-${sessionId}`,firstObservedAt:'2026-10-15T12:00:00Z',lastObservedAt:'2026-10-15T12:00:00Z'}],backgroundTasks:[],truncated:false,stale:false};
}
const mapDialog=()=>document.querySelector<HTMLElement>('[role="dialog"][aria-modal="true"]');
const emit=async(key:string,value?:any)=>{await act(async()=>mock.events.get(key)?.(value));};
const select=async(id:string)=>{await act(async()=>{mock.terminalProps.onOpenTerminalsChange([pane(id)]);mock.terminalProps.onFocusedAgentSessionChange(id);});};
const inventory=async(value:any[])=>{await act(async()=>mock.terminalProps.onOpenTerminalsChange(value));};
const openCounter=async()=>{const button=document.querySelector<HTMLButtonElement>('[data-testid="usage-bar"] [aria-haspopup="dialog"]');expect(button).not.toBeNull();await act(async()=>button!.click());};
const pane=(sessionId:string)=>({terminalId:`term-${sessionId}`,laraSessionId:sessionId,label:sessionId,hint:'',active:true});
it.each([false,true])('main/detached=%s removes stale footer activity after the last agent pane closes but allows explicit history inspection',async detached=>{
 mock.enabled=true;mock.detached=detached;await mount();if(!detached)await click('sidebar.nav.terminal');await inventory([pane('selected')]);await select('selected');
 expect(document.querySelector('[data-testid="usage-bar"] [aria-haspopup="dialog"]')).not.toBeNull();
 await inventory([]);await emit('agent:activity-changed',{...activity('selected',2),messageCode:'observation-ended'});
 expect(document.querySelector('[data-testid="usage-bar"] [aria-haspopup="dialog"]')).toBeNull();
 mock.getActivity.mockImplementation(async(id:string)=>({...activity(id),messageCode:'observation-ended'}));
 await click('Map menu root');expect(mapDialog()?.textContent).toContain('agents.map.messages.observationEnded');
 expect(document.querySelector('[data-testid="usage-bar"] [aria-haspopup="dialog"]')).toBeNull();
});
it.each([false,true])('main/detached=%s pins menu map while selected counter changes and keeps both leases independent',async detached=>{
 mock.enabled=true;mock.detached=detached;vi.useFakeTimers();await mount();if(!detached)await click('sidebar.nav.terminal');
 await select('selected');await click('Map menu root');
 expect(mapDialog()?.textContent).toContain('menu-root');expect(mapDialog()?.closest('[inert]')).toBeNull();
 await select('other');expect(mapDialog()?.textContent).toContain('menu-root');
 expect(mock.interests.mock.calls.map(([interest])=>[interest.purpose,interest.sessionId])).toEqual(expect.arrayContaining([['counter','selected'],['counter','other'],['dialog','menu-root']]));
 const windowIds=new Set(mock.interests.mock.calls.map(([i])=>i.windowId));expect(windowIds.size).toBe(1);
 await act(async()=>vi.advanceTimersByTime(10000));expect(mock.interests.mock.calls.slice(-2).map(([i])=>[i.purpose,i.sessionId])).toEqual(expect.arrayContaining([['counter','other'],['dialog','menu-root']]));
 if(detached)expect(mock.auto).toEqual({});
 const removes=mock.removeInterest.mock.calls.filter(([,purpose])=>purpose==='dialog').length;
 expect(removes).toBe(0);await click('agents.map.close');expect(mapDialog()).toBeNull();
 expect(mock.removeInterest.mock.calls.filter(([,purpose])=>purpose==='dialog')).toHaveLength(1);
 expect(mock.interests.mock.calls.at(-1)?.[0]).toMatchObject({purpose:'counter',sessionId:'other'});
});
it.each([false,true])('main/detached=%s opens counter root; explicit terminal action wins focus without a launch',async detached=>{
 mock.enabled=true;mock.detached=detached;await mount();if(!detached)await click('sidebar.nav.terminal');await select('selected');await inventory([pane('selected')]);
 if(!detached)await act(async()=>host.querySelector<HTMLInputElement>('[aria-label="agent filter"]')!.focus());
 const opener=document.querySelector<HTMLButtonElement>('[aria-haspopup="dialog"]')!;opener.focus();await act(async()=>opener.click());
 expect(mapDialog()?.textContent).toContain('selected');await click('agents.map.openTerminal');expect(mapDialog()).toBeNull();
 expect(mock.terminalProps.focusAgentRequest).toMatchObject({sessionId:'selected'});expect(mock.terminalProps.openRequest).toBeNull();expect(document.activeElement).toBe(document.querySelector('[aria-label="PTY input"]'));
 if(!detached)expect(mock.terminalProps.activeSurface).toBe('terminal');
});
it.each([undefined,'foreign'])('does not enable root navigation for matching PTY ID with session identity %s',async laraSessionId=>{
 mock.enabled=true;await go();await select('selected');await inventory([{...pane('selected'),laraSessionId}]);await act(async()=>mock.agentsProps.onOpenMap({id:'selected'}));
 const button=[...document.querySelectorAll('button')].find(b=>b.textContent==='agents.map.openTerminal')!;expect(button.disabled).toBe(true);await act(async()=>button.click());expect(mock.terminalProps.focusAgentRequest).toBeNull();expect(mock.terminalProps.openRequest).toBeNull();
});
it.each([false,true])('main/detached=%s keeps an ended-root map open but reset and feature disable close it',async detached=>{
 mock.enabled=true;mock.detached=detached;await mount();if(!detached)await click('sidebar.nav.terminal');await select('selected');await inventory([pane('selected')]);await openCounter();
 await inventory([]);await emit('agent:activity-changed',{...activity('selected',2),messageCode:'observation-ended'});expect(mapDialog()?.textContent).toContain('agents.map.messages.observationEnded');
 expect([...document.querySelectorAll('button')].find(b=>b.textContent==='agents.map.openTerminal')!.disabled).toBe(true);
 await emit('agent:sessions-reset');expect(mapDialog()).toBeNull();await select('next');await click('Map menu root');expect(mapDialog()).not.toBeNull();await emit('agent:feature-changed',false);expect(mapDialog()).toBeNull();
});
it('document decisions exclude map opening, and map closing leaves later dirty document choices unresolved',async()=>{
 mock.enabled=true;await go();await click('Edit buffer');await click('Close document');const decision=mapDialog();expect(decision).not.toBeNull();
 await act(async()=>mock.agentsProps.onOpenMap({id:'menu-root'}));expect(mapDialog()).toBe(decision);expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
 await click('documents.close.cancel');await click('Map menu root');expect(mapDialog()?.textContent).toContain('menu-root');await click('agents.map.close');await click('Close document');
 expect(mapDialog()?.textContent).toContain('documents.close');expect(mock.controller.buffers[0].text).toBe('unsaved');
});
it('map blocks real auto-switch and custom shortcuts until dismissal releases queued needs-input',async()=>{
 mock.enabled=true;mock.realAuto=true;localStorage.setItem('laraenv.agentAutoSwitch','true');vi.useFakeTimers();await go();await select('selected');await click('Map menu root');
 await emit('agent:session-changed',{id:'waiting',state:'needs-input',terminalId:'term-waiting'});await act(async()=>vi.advanceTimersByTime(1000));expect(mock.terminalProps.focusAgentRequest).toBeNull();
 await act(async()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'x',bubbles:true,cancelable:true})));expect(mock.customRun).not.toHaveBeenCalled();
 await click('agents.map.close');await act(async()=>vi.advanceTimersByTime(500));expect(mock.terminalProps.focusAgentRequest?.sessionId).toBe('waiting');
});

it.each([false,true])('main/detached=%s leaves the footer empty when there is no eligible agent pane',async detached=>{
 mock.enabled=true;mock.detached=detached;localStorage.setItem('laraenv.agentNavigatorVisible','false');await mount();if(!detached){await click('sidebar.nav.terminal');await click('workspace.navigation.hide');}
 expect(document.querySelector('[data-testid="usage-bar"] [aria-haspopup="dialog"]')).toBeNull();expect(mapDialog()).toBeNull();
 expect(mock.terminalProps.focusAgentRequest).toBeNull();expect(mock.terminalProps.openRequest).toBeNull();expect(mock.interests).not.toHaveBeenCalled();
});
it('shutdown closes inspection before opening its independent dirty-document decision',async()=>{
 mock.enabled=true;await go();await click('Edit buffer');await click('Map menu root');
 await emit('documents:shutdown-request',{requestId:'map-quit'});expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);expect(mapDialog()?.textContent).toContain('documents.close.title');
 expect(document.activeElement?.textContent).toBe('documents.close.cancel');await click('documents.close.cancel');expect(mock.controller.buffers[0].text).toBe('unsaved');expect(mock.terminalProps.openRequest).toBeNull();
});

it.each([false,true])('main/detached=%s ordinary counter dismissal restores focus after removing background inert',async detached=>{
 mock.enabled=true;mock.detached=detached;await mount();if(!detached)await click('sidebar.nav.terminal');await select('selected');
 const opener=document.querySelector<HTMLButtonElement>('[data-testid="usage-bar"] [aria-haspopup="dialog"]')!;
 for(const dismissal of ['close','Escape','backdrop']) {
  opener.focus();await act(async()=>opener.click());expect(opener.closest('[inert]')).not.toBeNull();expect(document.activeElement).not.toBe(opener);
  if(dismissal==='close')await click('agents.map.close');
  else await act(async()=>{if(dismissal==='Escape')document.activeElement!.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));else mapDialog()!.parentElement!.click();});
  expect(mapDialog()).toBeNull();expect(opener.closest('[inert]')).toBeNull();expect(document.activeElement).toBe(opener);
 }
});

it.each([false,true])('main/detached=%s remembers the counter before native inert can blur it',async detached=>{
 mock.enabled=true;mock.detached=detached;await mount();if(!detached)await click('sidebar.nav.terminal');await select('selected');
 const opener=document.querySelector<HTMLButtonElement>('[data-testid="usage-bar"] [aria-haspopup="dialog"]')!;
 // jsdom has no native inert focus behavior; emulate the browser boundary.
 const setAttribute=Element.prototype.setAttribute;
 vi.spyOn(Element.prototype,'setAttribute').mockImplementation(function(this:Element,name:string,value:string){setAttribute.call(this,name,value);if(name==='inert' && this.contains(document.activeElement))(document.activeElement as HTMLElement).blur();});
 opener.focus();await act(async()=>opener.click());expect(mapDialog()).not.toBeNull();await click('agents.map.close');expect(document.activeElement).toBe(opener);
});
