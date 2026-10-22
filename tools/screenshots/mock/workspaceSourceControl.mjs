import { buildInitScript } from './base.mjs';
import { BASE_STATE, mergeState } from './state.mjs';

export function sourceControlScript() {
  const context = name => ({ kind: 'project', projectName: name });
  const state = mergeState(BASE_STATE, {
    version: '0.5.9', webgl: false, agents: { enabled: true, profiles: [], sessions: [], usage: [] },
    projects: ['worktree-a', 'worktree-b'].map(name => ({ name, path: `C:/fake/${name}`, url: `http://${name}.test`, proxyPort: 0 })),
    workspace: { version: 2, groups: [{ x: 0, y: 0, w: 900, h: 600, snap: 'max', activeTabIndex: 0, tabs: ['worktree-a', 'worktree-b'].map(name => ({ label: name, activeLeafIndex: 0, tree: { type: 'leaf', label: name, kind: 'pwsh', spawn: { kind: 'project', projectName: name, terminalKind: 'pwsh' } } })) }] },
    documents: { version: 1, visible: true, panelPercent: 45, activeTabIndex: 0, tabs: [{ mode: 'source', references: [{ context: context('worktree-a'), rootKey: 'a'.repeat(64), relativePath: 'README.md' }] }] },
  });
  return buildInitScript({ state }) + `
    const base = window.go.main.App, copy = value => JSON.parse(JSON.stringify(value));
    const s = window.__sc = { calls: [], spawns: [], writes: [], stops: [], lists: [], saves: [], operations: {}, failCommit: true, holdStatus: false, holdSave: false, releaseStatus: null, releaseSave: null, holdList: false, releaseList: null, reports: [], replies: [], holdDiff: false, releaseDiff: null, reviewText: 'WORKTREE text' };
    const roots = { 'worktree-a': 'a'.repeat(64), 'worktree-b': 'b'.repeat(64) };
    const root = name => ({ rootKey: roots[name], label: name, path: 'C:/fake/' + name, canonicalRoot: 'C:/fake/' + name, canRead: true, canWrite: true });
    const repo = name => ({ context: { kind: 'project', projectName: name, sessionId: '' }, rootKey: roots[name], repositoryKey: 'repository-' + name });
    const entry = { id: 'readme', relativePath: 'README.md', originalPath: '', displayPath: 'README.md', indexCode: 'M', worktreeCode: 'M', kind: 'tracked', submoduleState: '', headOID: 'head-blob', indexOID: 'index-blob', headMode: '100644', indexMode: '100644', worktreeMode: '100644', canStage: true, canUnstage: true, disabledReason: '' };
    const snapshot = name => ({ repository: repo(name), root: root(name), snapshotRevision: 'snapshot-1', indexRevision: 'index-1', headOID: 'head-1', branch: 'main', upstream: '', gitOperation: '', unborn: false, detached: false, countsKnown: true, hasConflicts: false, busy: false, ahead: 1, behind: 0, entries: [entry, ...(s.extraEntries || [])], remotes: [{ id: 'origin-id', name: 'origin', displayURL: 'C:/fixtures/remote.git', canPush: true, disabledReason: '' }], operationId: '' });
    const files = s.files = Object.fromEntries(Object.keys(roots).map(name => [name, { canonicalPath: 'C:/fake/' + name + '/README.md', text: name + '\\nworking text', revision: 'disk-1', size: 28, bom: false, eol: 'lf', readOnly: false }]));
    const ok = (field, value) => ({ code: 'ok', message: '', [field]: copy(value) });
    const record = (method, request) => s.calls.push({ method, request: copy(request) });
    const start = (kind, request) => {
      record(kind, request);
      const id = 'operation-' + s.calls.length;
      const failed = kind === 'commit' && s.failCommit;
      const operation = { id, repository: copy(request.repository), kind, state: failed ? 'failed' : 'succeeded', resultCode: failed ? 'git_error' : 'ok', output: failed ? 'fixture hook refused commit' : 'fixture ' + kind + ' complete', message: '', sequence: 2, outputTruncated: false, hasExitCode: true, exitCode: failed ? 1 : 0, startedAt: '2026-10-09T10:00:00Z', finishedAt: '2026-10-09T10:00:01Z', commitOID: kind === 'commit' && !failed ? 'commit-2' : '', remote: request.remoteId || '', remoteBranch: request.remoteBranch || '', mutationObserved: !failed, remoteUncertain: false, afterSnapshot: snapshot(request.repository.context.projectName) };
      s.operations[id] = operation;
      // The production subscriber receives completion before the start promise resolves.
      window.__harness.emit('sourcecontrol:operation', { operationId: id, repository: copy(request.repository), sequence: 2, state: operation.state, stream: 'stdout', chunk: operation.output });
      return Promise.resolve(ok('operation', { ...operation, state: 'running', sequence: 1, finishedAt: '', output: '', afterSnapshot: null }));
    };
    const startProjectPty = name => { s.spawns.push(name); return Promise.resolve('pty-' + name); };
    const actions = {
      StartPtyForProject: startProjectPty,
      StartPtyForProjectCommand: startProjectPty,
      WritePty: (id, data) => { s.writes.push({ id, data }); return Promise.resolve(); },
      StopPty: id => { s.stops.push(id); return Promise.resolve(); },
      GetWorkspaceFileContext: ctx => Promise.resolve(ok('context', root(ctx.projectName))),
      ListWorkspaceDirectory: async (ctx, key, relative, cursor) => {
        s.lists.push(copy({ ctx, key, relative, cursor }));
        if (s.holdList && cursor) await new Promise(resolve => { s.releaseList = resolve; });
        const item = (name, kind = 'directory') => ({ name, relativePath: relative ? relative + '/' + name : name, kind });
        return ok('directory', { entries: relative ? [item('nested.txt', 'file')] : cursor ? [item('Folder500'), item('README.md', 'file'), item('zeta.txt', 'file')] : Array.from({ length: 500 }, (_, i) => item('Folder' + String(i).padStart(3, '0'))), nextCursor: relative || cursor ? '' : 'opaque-next-page' });
      },
      ReadWorkspaceDocument: ctx => Promise.resolve(ok('document', files[ctx.projectName])),
      CheckWorkspaceDocument: (ctx, key, relative, revision) => Promise.resolve(files[ctx.projectName].revision === revision ? { code: 'unchanged', message: '' } : ok('document', files[ctx.projectName])),
      SaveWorkspaceDocument: async (ctx, key, relative, text, revision) => { s.saves.push(copy({ ctx, key, relative, text, revision })); if (s.holdSave) await new Promise(resolve => { s.releaseSave = resolve; }); const file = files[ctx.projectName]; file.text = text; file.revision = 'disk-' + (s.saves.length + 1); return ok('document', file); },
      SetDocumentWorkspaceState: value => { s.reports.push(copy(value)); return Promise.resolve(); },
      ResolveDocumentShutdown: value => { s.replies.push(copy(value)); return Promise.resolve({ released: false }); },
      InspectSourceControl: ctx => { record('inspect', ctx); return Promise.resolve(ok('snapshot', snapshot(ctx.projectName))); },
      GetSourceControlStatus: async ref => { record('status', ref); if (s.holdStatus && ref.context.projectName === 'worktree-a') await new Promise(resolve => { s.releaseStatus = resolve; }); return ok('snapshot', snapshot(ref.context.projectName)); },
      ReadSourceControlDiff: async req => { record('diff', req); const text = s.reviewText; if (s.holdDiff && req.repository.context.projectName === 'worktree-a') await new Promise(resolve => { s.releaseDiff = resolve; }); const side = text => ({ text, revision: text, oid: '', eol: 'lf', size: text.length, bom: false }); return Promise.resolve(ok('diff', { id: 'diff-' + req.layer + '-' + text, repository: req.repository, entryId: req.entryId, relativePath: 'README.md', originalPath: '', layer: req.layer, kind: 'text', original: side(req.layer === 'index' ? 'HEAD text' : 'INDEX text'), modified: side(req.layer === 'index' ? 'INDEX text' : text), canOpenWorktree: true, message: '' })); },
      StartSourceControlStage: req => start('stage', req), StartSourceControlUnstage: req => start('unstage', req), StartSourceControlCommit: req => start('commit', req), StartSourceControlPush: req => start('push', req),
      GetSourceControlOperation: id => { record('operation', id); return Promise.resolve(ok('operation', s.operations[id])); },
      GetSourceControlBusy: () => Promise.resolve({ busy: false, operationIds: [] }),
    };
    window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || base[name] });
  `;
}
