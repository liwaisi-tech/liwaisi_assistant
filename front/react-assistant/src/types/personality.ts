export interface PersonalityResponse {
  user_id: string;
  principles: PrincipleResponse[];
  hierarchy: string[];
  tensions: TensionResponse[];
  version: number;
  updated_at: string;
}

export interface PrincipleResponse {
  kind: 'nucleo' | 'conducta' | 'etica';
  title: string;
  description: string;
  rules: string[];
}

export interface TensionResponse {
  between: [string, string];
  friction: string;
  resolution: string;
}

export interface UpdatePrincipleRequest {
  title?: string;
  description?: string;
  rules?: string[];
}

export interface SetHierarchyRequest {
  hierarchy: [string, string, string];
}

export interface ToolSummary {
  name: string;
  namespace: string;
  description: string;
  input_color: string;
  output_color: string;
  requires_hitl: boolean;
  version: string;
  toolbox: string;
  hashtags: string[];
}

export interface ToolListResponse {
  tools: ToolSummary[];
}
