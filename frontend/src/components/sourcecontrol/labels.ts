import type { GitDiff, GitLayer, GitOperation, StageDirtyChoice } from './types';

export const layerLabels = { index: 'git.index', worktree: 'git.worktree' } satisfies Record<GitLayer, string>;
export const selectionLabels = { index: 'git.select.index', worktree: 'git.select.worktree' } satisfies Record<GitLayer, string>;
export const operationLabels = { stage: 'git.operation.stage', unstage: 'git.operation.unstage', commit: 'git.operation.commit', push: 'git.operation.push' } satisfies Record<GitOperation['kind'], string>;
export const stateLabels = { running: 'git.state.running', cancelling: 'git.state.cancelling', succeeded: 'git.state.succeeded', failed: 'git.state.failed', cancelled: 'git.state.cancelled', uncertain: 'git.state.uncertain' } satisfies Record<GitOperation['state'], string>;
export const summaryLabels = { binary: 'git.summary.binary', too_large: 'git.summary.too_large', gitlink: 'git.summary.gitlink', unsupported: 'git.summary.unsupported' } satisfies Record<Exclude<GitDiff['kind'], 'text'>, string>;
export const choiceLabels = { 'save-and-stage': 'git.choice.save-and-stage', 'stage-disk': 'git.choice.stage-disk', cancel: 'git.choice.cancel' } satisfies Record<StageDirtyChoice, string>;
