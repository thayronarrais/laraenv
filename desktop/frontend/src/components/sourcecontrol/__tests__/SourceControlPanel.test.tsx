// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { SourceControlPanel } from '../SourceControlPanel';
import type { GitOperation, SourceControlController } from '../types';
import { entry, snapshot } from './uiFixtures';
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(cleanup);
const controller = (): SourceControlController => ({ snapshot: snapshot(), loading: false, error: null, query: '', message: 'draft', selectedIndexIds: ['one'], selectedWorktreeIds: [], operations: [], refresh: vi.fn(), setQuery: vi.fn(), setMessage: vi.fn(), select: vi.fn(), stage: vi.fn(), unstage: vi.fn(), commit: vi.fn(), push: vi.fn(), cancelOperation: vi.fn() });
const props = { onOpenReview: vi.fn(), onOpenDocument: vi.fn(), onClaimNavigator: vi.fn() };
it('summarizes successful actions once while preserving expandable history and visible errors and cancellation', () => {
  const c = controller();
  const operation = (id: string, state: GitOperation['state'], finishedAt: string) => ({ id, state, kind: 'stage', repository: c.snapshot!.repository, startedAt: finishedAt, finishedAt, message: `${id} result`, output: `${id} output`, outputTruncated: false } as GitOperation);
  c.operations = [operation('latest', 'succeeded', '2026-10-09T10:03:00Z'), operation('old', 'succeeded', '2026-10-09T10:01:00Z'), operation('middle', 'succeeded', '2026-10-09T10:02:00Z'), operation('active', 'running', ''), operation('failed', 'failed', ''), operation('uncertain', 'uncertain', '')];
  const view = render(<SourceControlPanel controller={c} {...props} />);
  expect(screen.queryByText('old result')).toBeNull();
  expect(screen.queryByText('latest output')).toBeNull();
  expect(screen.getByText('failed output')).toBeTruthy(); expect(screen.getByText('uncertain output')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'git.cancel' })); expect(c.cancelOperation).toHaveBeenCalledWith('active');
  const history = screen.getByRole('button', { name: /git.operationHistory/ });
  expect(history.getAttribute('aria-expanded')).toBe('false'); expect(history.getAttribute('title')).toContain('latest result');
  fireEvent.click(history); expect(screen.getByText('old result')).toBeTruthy(); expect(screen.getByText('latest output')).toBeTruthy();
  c.snapshot = { ...c.snapshot!, repository: { ...c.snapshot!.repository, rootKey: 'other' } };
  view.rerender(<SourceControlPanel controller={c} {...props} />);
  expect(screen.queryByText('old result')).toBeNull(); expect(screen.getByText('failed output')).toBeTruthy();
});
it('tree separates MM layers and exact selections', () => {
  const c = controller(); render(<SourceControlPanel controller={c} {...props} />);
  const index = within(screen.getByRole('group', { name: 'git.index' })); const worktree = within(screen.getByRole('group', { name: 'git.worktree' }));
  expect((index.getByRole('checkbox') as HTMLInputElement).checked).toBe(true); expect((worktree.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
  fireEvent.click(worktree.getByRole('checkbox')); expect(c.select).toHaveBeenCalledWith('worktree', 'one', true);
  fireEvent.click(index.getByRole('button', { name: /git.review/ })); expect(props.onOpenReview).toHaveBeenCalledWith(c.snapshot!.repository, expect.objectContaining({ snapshotRevision: 's1' }), 'one', 'index', { label: 'Checkout P', path: '/p' });
  fireEvent.click(worktree.getByRole('button', { name: /git.stageFile/ })); expect(c.stage).toHaveBeenCalledWith(['one']);
});
it.each([
  ['"unsafe/bad\\xff.txt"', '"unsafe', 'bad\\xff.txt"'],
  ['unsafe/CON.txt', 'unsafe', 'CON.txt'],
])('shows and searches blocked display name %s without treating it as a document path', (displayPath, folder, name) => {
  const c = controller(); c.query = 'unsafe'; c.snapshot!.entries = [{ ...entry(), relativePath: '', displayPath, kind: 'blocked', canStage: false, canUnstage: false }];
  const open = vi.fn(); render(<SourceControlPanel controller={c} {...props} onOpenDocument={open} />);
  const worktree = within(screen.getByRole('group', { name: 'git.worktree' }));
  expect(worktree.getByText(folder)).toBeTruthy();
  expect(worktree.getByRole('button', { name: `git.review ${displayPath}` }).textContent).toBe(name);
  const button = worktree.getByRole('button', { name: `git.openFile ${displayPath}` }) as HTMLButtonElement;
  expect(button.disabled).toBe(true); fireEvent.click(button); expect(open).not.toHaveBeenCalled();
  expect((worktree.getByRole('checkbox', { name: `git.select.worktree ${displayPath}` }) as HTMLInputElement).disabled).toBe(true);
});

it('git panel exposes whole-index destination and preserves failure draft', () => {
  const c = controller(); c.error = { code: 'git_error', message: 'hook rejected' }; render(<SourceControlPanel controller={c} {...props} />);
  expect(screen.getByText('git.wholeIndex')).toBeTruthy(); expect(screen.getByText('main')).toBeTruthy(); expect(screen.getByRole('alert').textContent).toContain('hook rejected');
  fireEvent.click(screen.getByRole('button', { name: 'git.commit' })); expect(c.commit).toHaveBeenCalledOnce(); expect(c.setMessage).not.toHaveBeenCalled();
});
it('push requires explicit remote and sends full branch ref with chosen upstream', () => {
  const c = controller(); render(<SourceControlPanel controller={c} {...props} />);
  expect((screen.getByRole('button', { name: 'git.push' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('git.remote'), { target: { value: 'r' } }); fireEvent.change(screen.getByLabelText('git.remoteBranch'), { target: { value: 'feature/nested' } }); fireEvent.click(screen.getByLabelText('git.setUpstream'));
  fireEvent.click(screen.getByRole('button', { name: 'git.push' })); expect(c.push).toHaveBeenCalledWith({ remoteId: 'r', remoteBranch: 'refs/heads/feature/nested', setUpstream: true });
});
it.each(['', '   '])('guards an empty commit message %j', message => {
  const c = controller(); c.message = message; render(<SourceControlPanel controller={c} {...props} />);
  fireEvent.click(screen.getByRole('button', { name: 'git.commit' })); expect(c.commit).not.toHaveBeenCalled();
});
it.each(['+main', ':main', 'refs/tags/v1', 'a..b', 'a.lock', 'a/*', 'a b', 'a/'])('refuses unsafe push target %s', branch => {
  const c = controller(); render(<SourceControlPanel controller={c} {...props} />);
  fireEvent.change(screen.getByLabelText('git.remote'), { target: { value: 'r' } }); fireEvent.change(screen.getByLabelText('git.remoteBranch'), { target: { value: branch } });
  fireEvent.click(screen.getByRole('button', { name: 'git.push' })); expect(c.push).not.toHaveBeenCalled();
});
it('stages selected whole files even when search hides part of the selection', () => {
  const c = controller(); c.snapshot!.entries.push(entry('two', 'assets/big.bin')); c.selectedWorktreeIds = ['one', 'two']; c.query = 'big.bin';
  render(<SourceControlPanel controller={c} {...props} />);
  expect(screen.queryByText('one.ts')).toBeNull(); expect(screen.getAllByText('assets')).toHaveLength(2);
  fireEvent.click(screen.getByRole('button', { name: 'git.stageSelected' })); expect(c.stage).toHaveBeenCalledWith(['one', 'two']);
});
it('shows clean and unavailable states and disables commit without an index', () => {
  const c = controller(); c.snapshot!.entries = []; const view = render(<SourceControlPanel controller={c} {...props} />);
  expect(screen.getByText('git.clean')).toBeTruthy(); expect((screen.getByRole('button', { name: 'git.commit' }) as HTMLButtonElement).disabled).toBe(true);
  view.rerender(<SourceControlPanel controller={{ ...c, snapshot: null }} {...props} />); expect(screen.getByText('git.unavailable')).toBeTruthy();
});

it('preserves nested folder and section collapse per physical checkout and layer through search and context gaps',()=> {
 const c=controller(); c.snapshot!.entries=[entry('a','src/deep/a.ts'),entry('b','src/b.ts'),entry('r','root.ts')]; c.selectedWorktreeIds=['a'];
 const view=render(<SourceControlPanel controller={c} {...props}/>);
 const tree=()=>within(screen.getByRole('group',{name:'git.worktree'}));
 const folder=()=>tree().getByRole('button',{name:'src',exact:true});
 expect(folder().getAttribute('aria-expanded')).toBe('true'); fireEvent.click(folder()); expect(tree().queryByRole('checkbox',{name:'git.select.worktree src/deep/a.ts'})).toBeNull();
 expect(within(screen.getByRole('group',{name:'git.index'})).getByRole('checkbox',{name:'git.select.index src/deep/a.ts'})).toBeTruthy();
 c.query='a.ts'; view.rerender(<SourceControlPanel controller={c} {...props}/>); expect((tree().getByRole('checkbox',{name:'git.select.worktree src/deep/a.ts'}) as HTMLInputElement).checked).toBe(true);
 fireEvent.click(folder());
 c.query=''; view.rerender(<SourceControlPanel controller={c} {...props}/>); expect(folder().getAttribute('aria-expanded')).toBe('false');
 const old=c.snapshot!; c.snapshot=null; view.rerender(<SourceControlPanel controller={c} {...props}/>); c.snapshot={...old,repository:{...old.repository,rootKey:'b'.repeat(64),repositoryKey:'repo-b'}}; view.rerender(<SourceControlPanel controller={c} {...props}/>); expect(folder().getAttribute('aria-expanded')).toBe('true');
 c.snapshot=old;view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(folder().getAttribute('aria-expanded')).toBe('false');
 fireEvent.click(tree().getByRole('button',{name:'git.worktree',exact:true}));expect(tree().queryByRole('button',{name:'src',exact:true})).toBeNull();
 c.query='a.ts';view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(tree().getByRole('button',{name:'src',exact:true})).toBeTruthy();c.query='';view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(tree().queryByRole('button',{name:'src',exact:true})).toBeNull();
});
it('folder action sends only eligible filtered descendant IDs and guards busy and blocked display paths',()=> {
 const c=controller();c.snapshot!.entries=[entry('deep','src/deep/a.ts'),entry('shallow','src/a.ts'),entry('hidden','src/b.ts'),{...entry('blocked',''),displayPath:'src/unsafe/a.ts',canStage:false,canUnstage:false,disabledReason:'unsupported'}];c.query='a.ts';
 const view=render(<SourceControlPanel controller={c} {...props}/>);const tree=()=>within(screen.getByRole('group',{name:'git.worktree'}));
 fireEvent.click(tree().getByRole('button',{name:'git.stageFolder src (2)',exact:true}));expect(c.stage).toHaveBeenCalledWith(['deep','shallow']);
 c.snapshot!.busy=true;view.rerender(<SourceControlPanel controller={c} {...props}/>);fireEvent.click(tree().getByRole('button',{name:'git.stageFolder src (2)',exact:true}));expect(c.stage).toHaveBeenCalledTimes(1);
 expect((tree().getByRole('button',{name:'git.openFile src/unsafe/a.ts'}) as HTMLButtonElement).disabled).toBe(true);
});

it.each(['constructor','toString','__proto__'])('treats %s as a normal folder and changes expansion on the first disclosure',folder=> {
 const c=controller();c.snapshot!.entries=[entry('ordinary',`${folder}/a.ts`)];render(<SourceControlPanel controller={c} {...props}/>);
 const tree=within(screen.getByRole('group',{name:'git.worktree'}));const disclosure=()=>tree.getByRole('button',{name:folder,exact:true});
 expect(disclosure().getAttribute('aria-expanded')).toBe('true');expect(tree.getByRole('button',{name:`git.review ${folder}/a.ts`,exact:true})).toBeTruthy();
 fireEvent.click(disclosure());expect(disclosure().getAttribute('aria-expanded')).toBe('false');expect(tree.queryByRole('button',{name:`git.review ${folder}/a.ts`,exact:true})).toBeNull();
 fireEvent.click(disclosure());expect(disclosure().getAttribute('aria-expanded')).toBe('true');fireEvent.click(tree.getByRole('button',{name:`git.stageFolder ${folder} (1)`,exact:true}));expect(c.stage).toHaveBeenCalledWith(['ordinary']);
});
it('ignores whitespace-only queries and normalizes search without losing collapsed ancestors or exact file IDs',()=> {
 const c=controller();c.snapshot!.entries=[entry('spaced','src/alpha  two.ts'),entry('plain','src/beta.ts')];const view=render(<SourceControlPanel controller={c} {...props}/>);
 const tree=()=>within(screen.getByRole('group',{name:'git.worktree'}));const section=()=>tree().getByRole('button',{name:'git.worktree',exact:true});const folder=()=>tree().getByRole('button',{name:'src',exact:true});
 fireEvent.click(folder());fireEvent.click(section());c.query='  ';view.rerender(<SourceControlPanel controller={c} {...props}/>);
 expect(section().getAttribute('aria-expanded')).toBe('false');expect(tree().queryByRole('button',{name:'src',exact:true})).toBeNull();
 c.query='  alpha  ';view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(section().getAttribute('aria-expanded')).toBe('true');expect(folder().getAttribute('aria-expanded')).toBe('true');expect(tree().getByRole('button',{name:'git.review src/alpha two.ts',exact:true}).getAttribute('title')).toBe('src/alpha  two.ts');
 fireEvent.click(tree().getByRole('button',{name:'git.stageFolder src (1)',exact:true}));expect(c.stage).toHaveBeenLastCalledWith(['spaced']);
 c.query='';view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(section().getAttribute('aria-expanded')).toBe('false');fireEvent.click(section());expect(folder().getAttribute('aria-expanded')).toBe('false');
 c.query='  ';view.rerender(<SourceControlPanel controller={c} {...props}/>);expect(folder().getAttribute('aria-expanded')).toBe('false');fireEvent.click(folder());
 expect(tree().getByRole('button',{name:'git.review src/alpha two.ts',exact:true}).getAttribute('title')).toBe('src/alpha  two.ts');expect(tree().getByRole('button',{name:'git.review src/beta.ts',exact:true})).toBeTruthy();fireEvent.click(tree().getByRole('button',{name:'git.stageFolder src (2)',exact:true}));expect(c.stage).toHaveBeenLastCalledWith(['spaced','plain']);
});
