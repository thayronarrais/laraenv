export function isUnavailableAgentHistoryError(error: unknown) {
  return String(error).toLowerCase().includes("provider conversation is unavailable");
}
