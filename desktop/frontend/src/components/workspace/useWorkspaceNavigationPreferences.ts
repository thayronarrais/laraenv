import { useCallback, useEffect, useState } from 'react';
import { readNavigationPreferences, updateNavigationPreferences, writeNavigationPreferences, type NavigationPreferencesPatch } from './navigationPreferences';
export function useWorkspaceNavigationPreferences() {
  const [preferences, setPreferences] = useState(readNavigationPreferences);
  useEffect(() => { writeNavigationPreferences(preferences); }, [preferences]);
  const update = useCallback((patch: NavigationPreferencesPatch) => setPreferences(current => updateNavigationPreferences(current, patch)), []);
  return { preferences, update };
}
