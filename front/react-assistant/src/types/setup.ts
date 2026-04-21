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
  // Forward-compatible fields per spec-architecture-model-selection-centralization.md
  // §4 — once the backend returns these, the UI prefers them.
  default?: string;
  available?: string[];
  // Registry metadata from the DB-backed model registry
  // (spec-architecture-model-registry-and-a2ui-management.md). Empty array
  // when the registry is disabled so existing consumers keep working.
  registry?: ModelRegistryEntry[];
}

// ── Model registry (spec-architecture-model-registry-and-a2ui-management.md) ──

export interface ModelRegistryEntry {
  id: string;
  registry_id: string;
  vendor: string;
  family: string;
  version: string;
  variant?: string;
  display_name: string;
  description: string;
  hugging_face_id?: string;
  modalities: { input: string[]; output: string[] };
  capabilities: ModelCapabilities;
  context: { length: number; tokenizer: string };
  pricing: { input_per_token: number; output_per_token: number; currency: string };
  supported_params: string[];
  default_params?: Record<string, unknown>;
  license: ModelLicense;
  lifecycle: ModelLifecycle;
  routes: ModelRoute[];
  source_metadata?: Record<string, unknown>;
  is_product_default: boolean;
  invokable: boolean;
  created_at: string;
  updated_at: string;
}

export interface ModelCapabilities {
  text: boolean;
  tools: boolean;
  streaming: boolean;
  reasoning: boolean;
  structured_output: boolean;
  vision: boolean;
  audio: boolean;
}

export interface ModelLicense {
  kind: string;
  spdx_id?: string;
  community_slug?: string;
  name?: string;
  url?: string;
  source: string;
  status: string;
  reviewed_by?: string;
  reviewed_at?: string;
}

export interface ModelLifecycle {
  state: string;
  registered_at: string;
  activated_at?: string;
  deprecated_at?: string;
  sunset_at?: string;
  replaced_by?: string;
  reason?: string;
}

export interface ModelRoute {
  provider_adapter: string;
  provider_model_id: string;
  endpoint_base_url?: string;
  priority: number;
  enabled: boolean;
  region?: string;
  is_moderated?: boolean;
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
