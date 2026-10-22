import { useEffect, useMemo, useSyncExternalStore } from 'react';
import type { SourceControlAPI } from './api';
import { GitReviewOperations, type GitReviewController } from './gitReviewOperations';
export type { GitReviewController, GitReviewTab } from './gitReviewOperations';
export function useGitReviews(api: SourceControlAPI): GitReviewController {
  const operations = useMemo(() => new GitReviewOperations(api), [api]);
  const state = useSyncExternalStore(operations.subscribe, operations.getSnapshot);
  useEffect(() => operations.dispose, [operations]);
  return { ...state, open: operations.open, select: operations.select, clearActive: operations.clearActive, close: operations.close };
}
