import { useState, useEffect, useCallback } from 'react';
import { getPersonality, updatePrinciple as apiUpdatePrinciple, setHierarchy as apiSetHierarchy, resetPersonality } from '../services/api';
import type { PersonalityResponse, UpdatePrincipleRequest } from '../types/personality';

export function usePersonality() {
  const [personality, setPersonality] = useState<PersonalityResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const loadPersonality = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getPersonality();
      setPersonality(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load personality');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPersonality();
  }, [loadPersonality]);

  const updatePrincipleHandler = useCallback(async (kind: string, data: UpdatePrincipleRequest) => {
    setSaving(true);
    setError(null);
    try {
      const updated = await apiUpdatePrinciple(kind, data);
      setPersonality(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update principle');
      throw err;
    } finally {
      setSaving(false);
    }
  }, []);

  const setHierarchyHandler = useCallback(async (hierarchy: [string, string, string]) => {
    setSaving(true);
    setError(null);
    try {
      const updated = await apiSetHierarchy({ hierarchy });
      setPersonality(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to set hierarchy');
      throw err;
    } finally {
      setSaving(false);
    }
  }, []);

  const resetHandler = useCallback(async () => {
    setSaving(true);
    setError(null);
    try {
      const updated = await resetPersonality();
      setPersonality(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to reset personality');
      throw err;
    } finally {
      setSaving(false);
    }
  }, []);

  return {
    personality,
    loading,
    error,
    saving,
    updatePrinciple: updatePrincipleHandler,
    setHierarchy: setHierarchyHandler,
    reset: resetHandler,
    reload: loadPersonality,
  };
}
