export interface UserProfile {
  id: string;
  email: string;
  name: string;
  picture: string;
  preferences: UserPreferences;
  onboarding_completed: boolean;
  created_at: string;
}

export interface UserPreferences {
  preferred_language: string;
  regional_variant?: string;
  preferred_model: string;
  model_overrides: Record<string, string>;
}

export interface UpdatePreferencesPayload {
  preferred_language?: string;
  regional_variant?: string;
  preferred_model?: string;
  model_overrides?: Record<string, string>;
}

export interface ModelRole {
  key: string;
  label: string;
  description: string;
  default_model: string;
}

export interface ModelsResponse {
  default_model: string;
  available_models: string[];
  roles: ModelRole[];
}

export type PersonalityPreset = 'balanced' | 'creative' | 'precise' | 'custom' | '';

export type RegionalVariant =
  | 'es-CO'
  | 'es-MX'
  | 'es-AR'
  | 'es-ES'
  | 'en-GB'
  | 'en-US'
  | 'en-AU';

/**
 * Variant options grouped by base language. The `defaultVariant` is the
 * preselected option when the user picks that base language during onboarding
 * (per spec REQ-003).
 */
export const REGIONAL_VARIANTS: Record<
  'es' | 'en',
  { defaultVariant: RegionalVariant; variants: ReadonlyArray<{ code: RegionalVariant; label: string }> }
> = {
  es: {
    defaultVariant: 'es-CO',
    variants: [
      { code: 'es-CO', label: 'Español (Colombia)' },
      { code: 'es-MX', label: 'Español (México)' },
      { code: 'es-AR', label: 'Español (Argentina)' },
      { code: 'es-ES', label: 'Español (España)' },
    ],
  },
  en: {
    defaultVariant: 'en-GB',
    variants: [
      { code: 'en-GB', label: 'English (UK)' },
      { code: 'en-US', label: 'English (US)' },
      { code: 'en-AU', label: 'English (Australia)' },
    ],
  },
} as const;

/**
 * Returns the default regional variant for a base UI language.
 * `es` → `es-CO`, `en` → `en-GB`, anything else → `es-CO` (global fallback).
 */
export function defaultVariantForLanguage(lang: string): RegionalVariant {
  const base = (lang || '').split('-')[0];
  if (base === 'en') return 'en-GB';
  if (base === 'es') return 'es-CO';
  return 'es-CO';
}

export interface OnboardingCompleteRequest {
  preferred_language: string;
  regional_variant: RegionalVariant;
  preferred_model: string;
  model_overrides: Record<string, string>;
  personality_preset: PersonalityPreset;
}
