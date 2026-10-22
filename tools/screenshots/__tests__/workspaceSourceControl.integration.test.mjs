import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import path from 'node:path';
import { mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { startServer } from '../serve.mjs';
import { sourceControlScript } from '../mock/workspaceSourceControl.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const proof = process.env.WORKSPACE_PROOF ? path.resolve(process.env.WORKSPACE_PROOF) : path.resolve(here, '../../../build/verification/workspace-source-control/final-fix');
let server, browser;
beforeAll(async () => {
  await mkdir(proof, { recursive: true });
  server = await startServer(path.resolve(here, '../../../frontend/dist'));
  browser = await chromium.launch({ headless: true, ...(process.env.WORKSPACE_CHROMIUM ? { executablePath: process.env.WORKSPACE_CHROMIUM } : {}) });
}, 30_000);
afterAll(async () => { await browser?.close(); await server?.close(); });

async function open(width = 1366) {
  const context = await browser.newContext({ viewport: { width, height: 768 } });
  const externalRequests = [], pageErrors = [], nativeDialogs = [];
  await context.route('**/*', route => {
    const url = route.request().url();
    if (/^https?:/.test(url) && new URL(url).origin !== new URL(server.url).origin) { externalRequests.push(url); return route.abort(); }
    return route.continue();
  });
  await context.addInitScript({ content: sourceControlScript() });
  const page = await context.newPage(); page.setDefaultTimeout(8_000);
  page.on('pageerror', error => pageErrors.push(String(error)));
  page.on('console', message => { if (message.type() === 'error') pageErrors.push(message.text()); });
  page.on('dialog', dialog => { nativeDialogs.push(dialog.message()); void dialog.dismiss(); });
  await page.goto(server.url, { waitUntil: 'domcontentloaded' });
  await page.getByRole('button', { name: 'Terminal', exact: true }).click();
  await page.locator('.xterm:visible').waitFor();
  await page.locator('[data-testid=document-surface] .monaco-editor:visible').waitFor();
  return { page, context, externalRequests, pageErrors, nativeDialogs };
}
const git = page => page.getByRole('region', { name: 'Source control', exact: true });
const group = (page, layer) => git(page).getByRole('group', { name: layer === 'index' ? 'Staged changes' : 'Working tree changes', exact: true });
const source = page => page.getByTestId('document-surface').locator('.monaco-editor:visible .view-lines').first();
const calls = (page, method) => page.evaluate(method => window.__sc.calls.filter(call => call.method === method), method);
const models = page => page.evaluate(() => window.monaco.editor.getModels().map(model => ({ id: model.id, uri: model.uri.toString(), text: model.getValue() })));
async function showTab(page, name) { const show = page.getByRole('button', { name: 'Show navigator', exact: true }); if(await show.isVisible()) await show.click(); await page.getByRole('tablist', {name:'Workspace navigation',exact:true}).getByRole('tab', { name, exact: true }).click(); }
async function showGit(page) { await showTab(page, 'Git'); await group(page, 'worktree').waitFor(); }
async function clean(run, name) {
  const missingBindings = await run.page.evaluate(() => window.__harness.missing);
  const receipt = { externalRequests: run.externalRequests, missingBindings, pageErrors: run.pageErrors, nativeDialogs: run.nativeDialogs };
  await writeFile(path.join(proof, `${name}-errors.json`), JSON.stringify(receipt, null, 2));
  expect(receipt).toEqual({ externalRequests: [], missingBindings: [], pageErrors: [], nativeDialogs: [] });
  expect(await run.page.getByText('Something went wrong', { exact: true }).count()).toBe(0);
}
async function measure(page, name, selector) {
  await page.evaluate(() => document.fonts.ready);
  const value = await page.evaluate(selector => {
    const rect = element => { const r = element.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right, bottom: r.bottom }; };
    return { widget: rect([...document.querySelectorAll(selector)].find(element => element.getClientRects().length > 0)), primary: rect(document.querySelector('aside')), navigator: rect(document.querySelector('aside[aria-label="Workspace navigation"]')), available: rect(document.querySelector('[data-testid=workspace-surfaces]')), panelPercent: document.querySelector('[role=separator][aria-label=Files]')?.getAttribute('aria-valuenow'), overflow: document.documentElement.scrollWidth > innerWidth };
  }, selector);
  await writeFile(path.join(proof, `${name}.json`), JSON.stringify(value, null, 2));
  await page.screenshot({ path: path.join(proof, `${name}.png`), animations: 'disabled' });
  expect(value.overflow).toBe(false); return value;
}
async function reachable(button) {
  await button.scrollIntoViewIfNeeded();
  const value = await button.evaluate(element => { const r = element.getBoundingClientRect(); const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2); return { reachable: element === hit || element.contains(hit), right: r.right, bottom: r.bottom, width: innerWidth, height: innerHeight }; });
  expect(value.reachable).toBe(true); expect(value.right).toBeLessThanOrEqual(value.width); expect(value.bottom).toBeLessThanOrEqual(value.height);
}

describe('Source control in the production browser with real Monaco and xterm', () => {
  it('keeps subtle corners and balanced gutters while terminals fill the available surface', async () => {
    const run = await open(), { page } = run;
    try {
      await showGit(page);
      for (let i = 1; i <= 3; i++) {
        await group(page, 'worktree').getByRole('button', { name: 'Stage file README.md', exact: true }).click();
        await git(page).getByRole('button', { name: `Operation history: Stage: Completed (${i})`, exact: true }).waitFor();
      }
      const history = git(page).getByRole('button', { name: 'Operation history: Stage: Completed (3)', exact: true });
      expect(await git(page).getByText('fixture stage complete', { exact: true }).count()).toBe(0);
      expect(await git(page).getByText('Stage: Completed', { exact: true }).count()).toBe(1);
      await history.click(); expect(await git(page).getByText('fixture stage complete', { exact: true }).count()).toBe(3);
      await history.click(); expect(await history.getAttribute('aria-expanded')).toBe('false');
      for (const collapsed of [false, true]) {
        if (collapsed) await page.getByRole('button', { name: 'Collapse navigation', exact: true }).click();
        const geometry = await page.evaluate(() => {
          const rail = document.querySelector('#primary-navigation'), area = document.querySelector('[data-testid=workspace-navigation-area]');
          const r = rail.getBoundingClientRect(), a = area.getBoundingClientRect(), workbench = rail.parentElement.getBoundingClientRect();
          return { left: r.left, right: a.right, gap: a.left - r.right, top: r.top - a.top, bottom: workbench.bottom - r.bottom, railRadius: getComputedStyle(rail).borderTopLeftRadius, areaRadius: getComputedStyle(area).borderTopLeftRadius, overflow: document.documentElement.scrollWidth > innerWidth };
        });
        expect(geometry).toEqual({ left: 8, right: 1358, gap: 8, top: 0, bottom: 8, railRadius: '8px', areaRadius: '8px', overflow: false });
        await page.screenshot({ path: path.join(proof, `continuous-${collapsed ? 'collapsed' : 'expanded'}.png`), animations: 'disabled' });
      }
      await page.getByRole('button', { name: 'Hide editor', exact: true }).click();
      const fills = await page.evaluate(() => {
        const surface = document.querySelector('[data-testid=terminal-surface]');
        return surface.getBoundingClientRect().width === document.querySelector('[data-testid=workspace-surfaces]').getBoundingClientRect().width && document.querySelector('[data-testid=terminal-group]').getBoundingClientRect().width === surface.clientWidth;
      });
      expect(fills).toBe(true);
      await page.screenshot({ path: path.join(proof, 'continuous-terminal-only.png'), animations: 'disabled' });
      await clean(run, 'continuous-workbench');
    } finally { await run.context.close(); }
  }, 30_000);
  it('aligns the navigation toggle and dashboard cards in both states and after resizing', async () => {
    const run = await open(), { page } = run;
    try {
      await page.getByRole('button', { name: 'Dashboard', exact: true }).click();
      let togglePosition;
      for (const width of [1366, 1280]) {
        await page.setViewportSize({ width, height: 768 });
        for (const collapsed of [false, true, false]) {
          const toggle = page.getByRole('button', { name: collapsed ? 'Expand navigation' : 'Collapse navigation', exact: true });
          if (!await toggle.isVisible()) await page.getByRole('button', { name: collapsed ? 'Collapse navigation' : 'Expand navigation', exact: true }).click();
          await reachable(toggle);
          const value = await page.evaluate(() => {
            const rail = document.querySelector('#primary-navigation'), main = document.querySelector('main'), toggle = rail.querySelector('button');
            const r = rail.getBoundingClientRect(), m = main.getBoundingClientRect(), b = toggle.getBoundingClientRect();
            const dashboard = [...main.children].find(el => el.style.display === 'block').firstElementChild;
            const cards = [...dashboard.querySelectorAll('section, .glass-strong')];
            return { gap: m.left - r.right, right: innerWidth - m.right, toggle: { x: b.x - r.x, y: b.y - r.y, width: b.width, height: b.height }, radii: cards.map(el => getComputedStyle(el).borderTopLeftRadius), inside: cards.every(el => { const c = el.getBoundingClientRect(); return c.left >= m.left && c.right <= m.right + 1 && c.bottom <= m.bottom + 1; }), overflow: document.documentElement.scrollWidth > innerWidth };
          });
          expect(value.gap).toBe(8); expect(value.right).toBe(8); expect(value.inside).toBe(true); expect(value.overflow).toBe(false);
          expect(new Set(value.radii)).toEqual(new Set(['8px']));
          expect(value.toggle.width).toBe(32); expect(value.toggle.height).toBe(32);
          if (togglePosition) expect(value.toggle).toEqual(togglePosition); else togglePosition = value.toggle;
          await writeFile(path.join(proof, `dashboard-${width}-${collapsed ? 'collapsed' : 'expanded'}.json`), JSON.stringify(value, null, 2));
          await page.screenshot({ path: path.join(proof, `dashboard-${width}-${collapsed ? 'collapsed' : 'expanded'}.png`), animations: 'disabled' });
        }
      }
      await clean(run, 'dashboard-navigation');
    } finally { await run.context.close(); }
  }, 30_000);
  it('one navigator and nested Git folders retain collapse, exact filtered actions and readable geometry with 181 changes', async () => {
    const run = await open(), {page} = run;
    try {
      for(const name of ['Agents','Files','Git']) expect(await page.getByRole('tab', {name, exact:true}).count()).toBe(1);
      for(const name of ['Agents','Files','Git']) expect(await page.getByRole('button', {name, exact:true}).count()).toBe(0);
      await page.evaluate(() => {
        const base={ originalPath:'', indexCode:'.', worktreeCode:'M', kind:'tracked', submoduleState:'', headOID:'head', indexOID:'index', headMode:'100644', indexMode:'100644', worktreeMode:'100644', canStage:true, canUnstage:false, disabledReason:'' };
        const file=(id,path)=>({...base,id,relativePath:path,displayPath:path});
        window.__sc.extraEntries=[file('nested-a','out/deep/a.ts'),file('nested-b','out/b.ts'),{...file('blocked',''),displayPath:'out/deep/bad\\xff.ts',canStage:false,disabledReason:'Unsupported filename'},...Array.from({length:177},(_,i)=>file('long-'+i,'attachments/a-very-long-folder-name-that-must-truncate/'+'nested/'.repeat(12)+'long-file-'+i+'.ts'))];
      });
      await showGit(page);await git(page).getByRole('button',{name:'Refresh',exact:true}).click();
      const work=group(page,'worktree');const out=()=>work.getByRole('button',{name:'out',exact:true});
      await out().waitFor();await work.getByRole('checkbox',{name:'Select working tree file out/deep/a.ts',exact:true}).check();
      await out().focus();await page.keyboard.press('Enter');expect(await out().getAttribute('aria-expanded')).toBe('false');expect(await work.getByRole('checkbox',{name:'Select working tree file out/deep/a.ts',exact:true}).count()).toBe(0);
      await git(page).getByLabel('Search changed paths',{exact:true}).fill('a.ts');
      expect(await work.getByRole('checkbox',{name:'Select working tree file out/deep/a.ts',exact:true}).isChecked()).toBe(true);
      const stage=work.getByRole('button',{name:'Stage files in folder out (1)',exact:true});await reachable(stage);await stage.click();
      await page.waitForFunction(()=>window.__sc.calls.some(c=>c.method==='stage'));
      expect((await calls(page,'stage')).at(-1).request.entries).toEqual([{entryId:'nested-a',diskRevision:''}]);
      await git(page).getByLabel('Search changed paths',{exact:true}).fill('');expect(await out().getAttribute('aria-expanded')).toBe('false');
      await showTab(page,'Files');await showGit(page);expect(await out().getAttribute('aria-expanded')).toBe('false');
      await page.getByTestId('terminal-surface').getByText('worktree-b',{exact:true}).click();await git(page).getByText('worktree-b',{exact:true}).waitFor();expect(await out().getAttribute('aria-expanded')).toBe('true');
      await page.getByTestId('terminal-surface').getByText('worktree-a',{exact:true}).click();await git(page).getByText('worktree-a',{exact:true}).waitFor();expect(await out().getAttribute('aria-expanded')).toBe('false');
      await page.getByRole('button',{name:'Hide navigator',exact:true}).click();const reopen=page.getByRole('button',{name:'Show navigator',exact:true});await reachable(reopen);expect(await reopen.getAttribute('aria-controls')).toBe('workspace-navigator');await reopen.click();expect(await out().getAttribute('aria-expanded')).toBe('false');
      await work.getByRole('button',{name:'Working tree changes',exact:true}).click();expect(await out().count()).toBe(0);await work.getByRole('button',{name:'Working tree changes',exact:true}).click();
      await out().click();const blocked=work.getByRole('button',{name:'Stage file out/deep/bad\\xff.ts',exact:true});expect(await blocked.isDisabled()).toBe(true);expect(await work.getByRole('button',{name:'Open file out/deep/bad\\xff.ts',exact:true}).isDisabled()).toBe(true);
      await git(page).getByLabel('Commit message',{exact:true}).fill('large checkout draft');await reachable(git(page).getByRole('button',{name:'Commit',exact:true}));
      expect((await measure(page,'polished-git-wide-1366','.monaco-editor')).widget.width).toBeGreaterThanOrEqual(400);
      const nav=page.getByRole('complementary',{name:'Workspace navigation',exact:true});
      await git(page).locator('.git-changes-scroll').evaluate(el=>{el.scrollTop=0;});
      for(const width of [240,420]) { const resize=nav.getByRole('separator',{name:'Resize navigator',exact:true});await resize.focus();await page.keyboard.press(width===240?'Home':'End');expect(await resize.getAttribute('aria-valuenow')).toBe(String(width));expect((await measure(page,'polished-git-sidebar-'+width,'.monaco-editor')).widget.width).toBeGreaterThanOrEqual(400);
        const long=work.getByRole('button',{name:/^Stage file attachments\/.*long-file-0\.ts$/});await reachable(long);
        expect(await long.evaluate(el=>{const row=el.closest('li');return row.scrollWidth<=row.clientWidth;})).toBe(true);
        await git(page).locator('.git-changes-scroll').evaluate(el=>{el.scrollTop=0;}); }
      await git(page).getByLabel('Search changed paths',{exact:true}).fill('out/');
      await measure(page,'polished-nested-actions','.monaco-editor');
      await git(page).getByLabel('Search changed paths',{exact:true}).fill('');
      await page.setViewportSize({width:1000,height:768});await page.getByRole('button',{name:'Collapse navigation',exact:true}).click();expect((await measure(page,'polished-git-drawer-1000','.monaco-editor')).widget.width).toBeGreaterThanOrEqual(400);
      await page.getByRole('button',{name:'Hide navigator',exact:true}).click();await page.getByRole('button',{name:'Show navigator',exact:true}).click();await reachable(git(page).getByRole('button',{name:'Push',exact:true}));
      expect(await page.evaluate(()=>window.__sc.stops)).toEqual([]);expect(await page.evaluate(()=>window.__sc.spawns)).toEqual(['worktree-a','worktree-b']);
      await clean(run,'polished-git');
    } finally {await run.context.close();}
  },60_000);
  it('fresh explicit reviews retain immutable models and captured checkout identity after deferred completion', async () => {
    const run = await open(), { page } = run;
    try {
      await showGit(page);
      const review = () => group(page, 'worktree').getByRole('button', { name: 'Review changes README.md', exact: true });
      await review().click();
      await page.waitForFunction(() => window.monaco.editor.getModels().some(m => m.getValue() === 'WORKTREE text'));
      const old = (await models(page)).filter(m => m.uri.startsWith('inmemory://git-review/'));
      await page.evaluate(() => { window.__sc.reviewText = 'NEW disk bytes'; window.__sc.holdDiff = true; });
      await git(page).getByRole('button', { name: 'Refresh', exact: true }).click();
      await review().click();
      await page.waitForFunction(() => !!window.__sc.releaseDiff);
      await page.getByTestId('terminal-surface').getByText('worktree-b', { exact: true }).click();
      await git(page).getByText('worktree-b', { exact: true }).waitFor();
      await review().click();
      await page.waitForFunction(() => window.monaco.editor.getModels().some(m => m.getValue() === 'NEW disk bytes'));
      await page.evaluate(() => { window.__sc.holdDiff = false; window.__sc.releaseDiff(); });
      await page.waitForFunction(() => window.monaco.editor.getModels().filter(m => m.uri.toString().startsWith('inmemory://git-review/')).length === 6);
      const b = page.getByRole('region', { name: 'Review changes · worktree-b · C:/fake/worktree-b · README.md · Working tree changes', exact: true });
      expect(await b.isVisible()).toBe(true);
      const aTabs = page.getByRole('button', { name: 'README.md · Working tree changes · worktree-a · C:/fake/worktree-a', exact: true });
      expect(await aTabs.count()).toBe(2);
      await aTabs.last().click();
      const a = page.getByRole('region', { name: 'Review changes · worktree-a · C:/fake/worktree-a · README.md · Working tree changes', exact: true }).locator('visible=true');
      expect(await a.isVisible()).toBe(true); expect(await a.locator('header').innerText()).toContain('C:/fake/worktree-a');
      expect(await aTabs.last().getAttribute('title')).toBe('C:/fake/worktree-a/README.md');
      expect(await git(page).getByText('worktree-b', { exact: true }).isVisible()).toBe(true);
      expect((await models(page)).filter(m => old.some(o => o.uri === m.uri))).toEqual(old);
      await page.getByTestId('terminal-surface').getByText('worktree-a', { exact: true }).click();
      await git(page).getByText('worktree-a', { exact: true }).waitFor();
      const count = (await models(page)).length;
      await review().click();
      await page.waitForFunction(() => window.__sc.calls.filter(c => c.method === 'diff').length === 4);
      await page.locator('.monaco-diff-editor:visible').waitFor();
      expect((await models(page)).length).toBe(count); expect(await aTabs.count()).toBe(2);
      const geometry = await measure(page, 'fresh-origin-wide', '.monaco-diff-editor');
      expect(geometry.widget.width).toBeGreaterThanOrEqual(300);
      const close = page.getByRole('button', { name: 'Close review README.md · worktree-a · C:/fake/worktree-a', exact: true });
      await reachable(close.last()); await close.last().click();
      expect((await models(page)).filter(m => old.some(o => o.uri === m.uri))).toEqual(old);
      await close.click();
      expect((await models(page)).filter(m => old.some(o => o.uri === m.uri))).toEqual([]);
      await page.getByRole('button', { name: 'Close review README.md · worktree-b · C:/fake/worktree-b', exact: true }).click();
      expect((await models(page)).filter(m => m.uri.startsWith('inmemory://git-review/'))).toEqual([]);
      await reachable(page.getByRole('button', { name: 'Source', exact: true }));
      await clean(run, 'fresh-origin');
    } finally { await run.context.close(); }
  }, 60_000);
  // A lost mount, wrong diff layer, missing pagination cursor, or collapsed editor fails this journey.
  it('preserves mounted terminals/models across navigation and renders paged files and readonly MM reviews', async () => {
    const run = await open(), { page } = run;
    try {
      await source(page).click(); await page.keyboard.type(' retained');
      const initial = await models(page);
      await page.waitForFunction(() => window.__harness.hasListener('term:data:pty-worktree-a'));
      await page.evaluate(() => window.__harness.emit('term:data:pty-worktree-a', 'terminal retained\r\n'));
      await showTab(page, 'Files');
      const files = page.getByRole('tabpanel', { name: 'Files', exact: true });
      await files.getByRole('button', { name: '▸ Folder000', exact: true }).waitFor();
      expect(await files.locator('button').count()).toBe(502); // Open, 500 directories, Load more.
      await page.evaluate(() => { window.__sc.holdList = true; });
      await files.getByRole('button', { name: 'Load more', exact: true }).click();
      await page.waitForFunction(() => !!window.__sc.releaseList);
      await showGit(page);
      await page.evaluate(() => { window.__sc.holdList = false; window.__sc.releaseList(); });
      await showTab(page, 'Files');
      expect(await files.getByRole('button', { name: 'zeta.txt', exact: true }).count()).toBe(0);
      await files.getByRole('button', { name: 'Load more', exact: true }).click();
      await files.getByRole('button', { name: 'zeta.txt', exact: true }).waitFor();
      const names = await files.locator('button').allTextContents();
      expect(names.slice(-3).map(name => name.trim())).toEqual(['▸ Folder500', 'README.md', 'zeta.txt']);
      await files.getByRole('button', { name: '▸ Folder000', exact: true }).click();
      await files.getByRole('button', { name: 'nested.txt', exact: true }).waitFor();
      expect(await page.evaluate(() => window.__sc.lists.map(({ relative, cursor, key }) => ({ relative, cursor, key })))).toEqual([
        { relative: '', cursor: '', key: 'a'.repeat(64) }, { relative: '', cursor: 'opaque-next-page', key: 'a'.repeat(64) }, { relative: '', cursor: 'opaque-next-page', key: 'a'.repeat(64) }, { relative: 'Folder000', cursor: '', key: 'a'.repeat(64) },
      ]);
      await showGit(page);
      const wide = await measure(page, 'wide-source-1366', '[data-testid=document-surface] .monaco-editor');
      expect(wide.primary.width).toBe(192); expect(wide.navigator.width).toBe(300); expect(wide.panelPercent).toBe('45'); expect(wide.widget.width).toBeGreaterThanOrEqual(300);
      await reachable(page.getByRole('button', { name: 'Save', exact: true }));
      await group(page, 'index').getByRole('button', { name: 'Review changes README.md', exact: true }).click();
      await page.locator('.monaco-diff-editor:visible').waitFor();
      await page.waitForFunction(() => window.monaco.editor.getModels().some(model => model.getValue() === 'HEAD text'));
      const wideDiff = await measure(page, 'wide-diff-1366', '.monaco-diff-editor');
      expect(wideDiff.widget.width).toBeGreaterThanOrEqual(400);
      await group(page, 'worktree').getByRole('button', { name: 'Review changes README.md', exact: true }).click();
      await page.waitForFunction(() => window.monaco.editor.getModels().some(model => model.getValue() === 'WORKTREE text'));
      const reviewModels = (await models(page)).filter(model => model.uri.startsWith('inmemory://git-review/'));
      expect(reviewModels.map(model => model.text)).toEqual(['HEAD text', 'INDEX text', 'INDEX text', 'WORKTREE text']);
      const writesBeforeReview = await page.evaluate(() => window.__sc.writes);
      const savesBeforeReview = await page.evaluate(() => window.__sc.saves);
      await page.locator('.monaco-diff-editor:visible textarea').last().focus(); await page.keyboard.type('forbidden'); await page.keyboard.press('Control+s');
      expect((await models(page)).filter(model => model.uri.startsWith('inmemory://git-review/'))).toEqual(reviewModels);
      expect(await page.evaluate(() => window.__sc.writes)).toEqual(writesBeforeReview);
      expect(await page.evaluate(() => window.__sc.saves)).toEqual(savesBeforeReview);
      await page.getByRole('button', { name: 'Collapse navigation', exact: true }).click();
      expect(await page.locator('aside').first().evaluate(el => el.getBoundingClientRect().width)).toBe(56);
      await page.getByRole('button', { name: 'Hide navigator', exact: true }).click();
      await showTab(page, 'Agents');
      await showGit(page);
      await page.getByRole('button', { name: 'Dashboard', exact: true }).click();
      await page.getByRole('button', { name: 'Terminal', exact: true }).click();
      expect((await models(page)).find(model => model.id === initial[0].id)).toEqual(initial[0]);
      expect((await models(page)).filter(model => model.uri.startsWith('inmemory://git-review/'))).toEqual(reviewModels);
      // xterm preserves the buffer while hidden; its DOM repaint follows the
      // visibility observer on a later frame. Wait for the observable text.
      await expect.poll(async () => (await page.locator('.xterm:visible').innerText()).replaceAll('\n', ''))
        .toContain('terminal retained');
      expect(await page.evaluate(() => window.__sc.spawns)).toEqual(['worktree-a', 'worktree-b']);
      // Dispose immediately after real text is present: no sleeps masking Monaco cleanup errors.
      await page.getByRole('button', { name: 'Close review README.md · worktree-a · C:/fake/worktree-a', exact: true }).last().click();
      await page.getByRole('button', { name: 'Close review README.md · worktree-a · C:/fake/worktree-a', exact: true }).click();
      expect((await models(page)).filter(model => model.uri.startsWith('inmemory://git-review/'))).toEqual([]);
      await clean(run, 'navigation');
    } finally { await run.context.close(); }
  }, 60_000);

  // A wrong captured checkout, dropped early event, auto-push, or modal key leak fails this journey.
  it('guards dirty decisions and keeps captured stage authority while the user switches checkout', async () => {
    const run = await open(), { page } = run;
    try {
      await source(page).click(); await page.keyboard.type(' dirty-A');
      await showGit(page);
      await group(page, 'worktree').getByRole('checkbox').check();
      await group(page, 'worktree').getByRole('button', { name: 'Stage selected', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Unsaved changes before staging' }); await dialog.waitFor();
      expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
      const before = await models(page);
      await page.keyboard.type('blocked'); await page.keyboard.press('Control+s'); await page.keyboard.press('Control+k');
      expect(await page.evaluate(() => window.__sc.writes)).toEqual([]); expect(await models(page)).toEqual(before); expect(await page.getByRole('dialog').count()).toBe(1);
      await page.evaluate(() => window.__harness.emit('documents:shutdown-request', { requestId: 'blocked-quit', intent: 'quit' }));
      await page.waitForFunction(() => window.__sc.replies.some(reply => reply.requestId === 'blocked-quit'));
      expect(await page.evaluate(() => window.__sc.replies.at(-1).decision)).toBe('cancel');
      // The application makes the background inert. Choose first; switch during the native refresh gate.
      await page.evaluate(() => { window.__sc.holdStatus = true; });
      await dialog.getByRole('button', { name: 'Stage disk version', exact: true }).click();
      await page.waitForFunction(() => !!window.__sc.releaseStatus);
      await page.getByTestId('terminal-surface').getByText('worktree-b', { exact: true }).click();
      await page.waitForFunction(() => window.__sc.calls.some(call => call.method === 'inspect' && call.request.projectName === 'worktree-b'));
      await page.evaluate(() => { window.__sc.holdStatus = false; window.__sc.releaseStatus(); });
      await page.waitForFunction(() => window.__sc.calls.some(call => call.method === 'stage'));
      expect((await calls(page, 'stage'))[0].request).toMatchObject({ repository: { context: { projectName: 'worktree-a' }, rootKey: 'a'.repeat(64), repositoryKey: 'repository-worktree-a' }, entries: [{ entryId: 'readme', diskRevision: '' }] });
      await page.getByTestId('terminal-surface').getByText('worktree-a', { exact: true }).click();
      await git(page).getByText('Stage: Completed', { exact: true }).waitFor();
      await group(page, 'index').getByRole('button', { name: 'Unstage file README.md', exact: true }).click();
      await git(page).getByText('Unstage: Completed', { exact: true }).waitFor();
      await git(page).getByLabel('Commit message', { exact: true }).fill('reviewed index');
      await reachable(git(page).getByRole('button', { name: 'Commit', exact: true }));
      await git(page).getByRole('button', { name: 'Commit', exact: true }).click();
      await git(page).getByText('fixture hook refused commit', { exact: true }).waitFor();
      expect(await git(page).getByRole('textbox', { name: /^Commit message/ }).inputValue()).toBe('reviewed index');
      expect(await calls(page, 'push')).toEqual([]);
      await page.evaluate(() => { window.__sc.failCommit = false; });
      await git(page).getByRole('button', { name: 'Commit', exact: true }).click();
      await page.waitForFunction(() => document.querySelector('textarea:not(.inputarea)')?.value === '');
      expect(await calls(page, 'push')).toEqual([]);
      await git(page).getByRole('combobox').selectOption('origin-id');
      await git(page).getByLabel('Remote branch', { exact: true }).fill('review/topic');
      await reachable(git(page).getByRole('button', { name: 'Push', exact: true }));
      await git(page).getByRole('button', { name: 'Push', exact: true }).click();
      await git(page).getByText('Push: Completed', { exact: true }).waitFor();
      expect((await calls(page, 'push'))[0].request).toMatchObject({ remoteId: 'origin-id', remoteBranch: 'refs/heads/review/topic', setUpstream: false });
      await clean(run, 'operations');
    } finally { await run.context.close(); }
  }, 60_000);

  it('rechecks dirty generations after pending save and retains source/reviews when the last PTYs exit', async () => {
    const run = await open(), { page } = run;
    try {
      await source(page).click(); await page.keyboard.type(' first'); await showGit(page);
      await group(page, 'worktree').getByRole('button', { name: 'Stage file README.md', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Unsaved changes before staging' });
      await page.evaluate(() => { window.__sc.holdSave = true; });
      await dialog.getByRole('button', { name: 'Save and stage' }).click();
      await page.waitForFunction(() => !!window.__sc.releaseSave);
      await page.evaluate(() => { const model = window.monaco.editor.getModels().find(m => m.uri.toString().startsWith('inmemory://workspace/')); model.setValue(model.getValue() + ' second'); });
      await page.evaluate(() => { window.__sc.holdSave = false; window.__sc.releaseSave(); });
      await page.waitForFunction(() => window.__sc.reports.at(-1)?.dirtyCount > 0);
      await page.getByRole('button', { name: 'Save', exact: true }).waitFor({ state: 'visible' });
      expect(await calls(page, 'stage')).toEqual([]);
      await group(page, 'index').getByRole('button', { name: 'Review changes README.md', exact: true }).click();
      await page.waitForFunction(() => window.monaco.editor.getModels().some(model => model.getValue() === 'HEAD text'));
      const retained = await models(page);
      await page.waitForFunction(() => window.__harness.hasListener('term:exit:pty-worktree-a') && window.__harness.hasListener('term:exit:pty-worktree-b'));
      await page.evaluate(() => { window.__harness.emit('term:exit:pty-worktree-b', 'done'); window.__harness.emit('term:exit:pty-worktree-a', 'done'); });
      await page.waitForFunction(() => document.querySelectorAll('.xterm').length === 0);
      expect(await models(page)).toEqual(retained);
      expect(retained.some(model => model.text.includes('second'))).toBe(true);
      expect(await page.getByRole('button', { name: /^Close review README.md/ }).isVisible()).toBe(true);
      await clean(run, 'last-pty');
    } finally { await run.context.close(); }
  }, 60_000);

  it('closes the compact drawer on document/review selection and keeps useful editor width', async () => {
    const run = await open(1000), { page } = run;
    try {
      await page.getByRole('tab', { name: 'Files', exact: true }).click();
      await page.getByLabel('Relative path', { exact: true }).fill('README.md');
      await page.getByRole('button', { name: 'Open', exact: true }).click();
      await page.getByRole('button', { name: 'Source', exact: true }).click();
      await source(page).waitFor();
      const narrow = await measure(page, 'compact-source-1000', '[data-testid=document-surface] .monaco-editor');
      expect(narrow.widget.width).toBeGreaterThanOrEqual(400);
      expect(await page.getByRole('complementary', { name: 'Workspace navigation' }).isVisible()).toBe(false);
      await showGit(page);
      await group(page, 'index').getByRole('button', { name: 'Review changes README.md' }).click();
      await page.locator('.monaco-diff-editor:visible').waitFor();
      const diff = await measure(page, 'compact-diff-1000', '.monaco-diff-editor');
      expect(diff.widget.width).toBeGreaterThanOrEqual(400);
      await reachable(page.getByRole('button', { name: 'Close review README.md · worktree-a · C:/fake/worktree-a', exact: true }));
      expect(await page.getByRole('region', { name: 'Review changes · worktree-a · C:/fake/worktree-a · README.md · Staged changes', exact: true }).isVisible()).toBe(true);
      expect(await page.evaluate(() => JSON.parse(localStorage.getItem('laraenv.workspaceNavigation')).sidebarWidth)).toBe(300);
      await clean(run, 'compact');
    } finally { await run.context.close(); }
  }, 60_000);

  it('keeps the global dirty dialog alive when its originating PTY exits', async () => {
    const run = await open(), { page } = run;
    try {
      await source(page).click(); await page.keyboard.type(' retained origin'); await showGit(page);
      await group(page, 'worktree').getByRole('button', { name: 'Stage file README.md', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Unsaved changes before staging' }); await dialog.waitFor();
      await page.waitForFunction(() => window.__harness.hasListener('term:exit:pty-worktree-a'));
      await page.evaluate(() => window.__harness.emit('term:exit:pty-worktree-a', 'done'));
      await page.waitForFunction(() => document.querySelectorAll('.xterm').length === 1);
      expect(await dialog.isVisible()).toBe(true);
      expect(await dialog.innerText()).toContain('C:/fake/worktree-a');
      expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
      await dialog.getByRole('button', { name: 'Stage disk version' }).click();
      await page.waitForFunction(() => window.__sc.calls.some(call => call.method === 'stage'));
      expect((await calls(page, 'stage'))[0].request.repository).toMatchObject({ rootKey: 'a'.repeat(64), repositoryKey: 'repository-worktree-a' });
      expect(await page.evaluate(() => window.__sc.saves)).toEqual([]);
      expect((await models(page)).some(model => model.text.includes('retained origin'))).toBe(true);
      await clean(run, 'dialog-survival');
    } finally { await run.context.close(); }
  }, 60_000);
});
