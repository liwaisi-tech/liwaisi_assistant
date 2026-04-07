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
  preferred_model: string;
  model_overrides: Record<string, string>;
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

export interface OnboardingCompleteRequest {
  preferred_language: string;
  preferred_model: string;
  model_overrides: Record<string, string>;
  personality_preset: PersonalityPreset;
}
