import { useEffect, useMemo, useSyncExternalStore } from 'react';
import type { DocumentController } from '../documents/types';
import { StageDocuments } from './stageDocuments';
export type { StageDirtyDecision } from './stageDocuments';
export function useStageDocuments(documents: DocumentController) {
  const { getBuffers, getBuffer, save } = documents;
  const coordinator = useMemo(() => new StageDocuments({ getBuffers, getBuffer, save }), [getBuffers, getBuffer, save]);
  const decision = useSyncExternalStore(coordinator.subscribe, coordinator.getDecision);
  useEffect(() => coordinator.dispose, [coordinator]);
  return { beforeStage: coordinator.beforeStage, decision, choose: coordinator.choose };
}
