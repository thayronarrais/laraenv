import type { ProblemCode } from './types';

// Keep the possible keys explicit so catalogue validation can verify them.
const keys: Record<ProblemCode, string> = {
  invalid_path: 'documents.problem.invalid_path',
  stale_cursor: 'documents.problem.invalid_path',
  unavailable: 'documents.problem.unavailable',
  too_large: 'documents.problem.too_large',
  unsupported_format: 'documents.problem.unsupported_format',
  permission_denied: 'documents.problem.permission_denied',
  conflict: 'documents.problem.conflict',
  io_error: 'documents.problem.io_error',
};

export const documentProblemKey = (code: string): string => keys[code as ProblemCode] ?? keys.io_error;
