export interface FlowSummary {
  hash: string;
  role: string;
  execution_count: number;
  success_rate: number;
  avg_cost_usd: number;
  avg_duration_ms: number;
  created_at: string;
  updated_at: string;
}

export interface FlowListResponse {
  items: FlowSummary[];
  has_more: boolean;
  next_cursor: string;
}

export interface FlowDetail {
  hash: string;
  role: string;
  topology: CPNTopology;
  stats: FlowStats;
  created_at: string;
  updated_at: string;
}

export interface FlowStats {
  execution_count: number;
  success_rate: number;
  avg_cost_usd: number;
  avg_duration_ms: number;
}

export interface CPNTopology {
  id: string;
  role: string;
  depth: number;
  mode: string;
  places: Record<string, PlaceTopology>;
  transitions: Record<string, TransitionTopology>;
}

export interface PlaceTopology {
  id: string;
  color: string;
  space: string;
}

export interface TransitionTopology {
  id: string;
  kind: string;
  inputPlaces: string[];
  outputPlaces: string[];
  errorPlace?: string;
  guardFunc?: string;
  executorFunc?: string;
  systemPrompt?: string;
  toolName?: string;
  llmConfig?: LLMConfigTopology;
  hitlConfig?: HITLConfigTopology;
  subNetTopology?: CPNTopology;
}

export interface LLMConfigTopology {
  model?: string;
  maxTokens?: number;
  temperature?: number;
  streamOutput?: boolean;
  requireJSON?: boolean;
  skipHistory?: boolean;
}

export interface HITLConfigTopology {
  prompt?: string;
  revisionLoop?: boolean;
}

export interface ExecutionRecord {
  id: string;
  cpn_id: string;
  session_id: string;
  transitions_fired: number;
  llm_calls: number;
  tool_calls: number;
  tokens_produced: number;
  total_cost_usd: number;
  duration_ms: number;
  success: boolean;
  started_at: string;
  completed_at: string;
}

export interface ExecutionEvent {
  id: string;
  type: string;
  transition_id: string;
  transition_kind: string;
  cpn_id: string;
  cpn_depth: number;
  cpn_role: string;
  payload?: unknown;
  token_snapshot?: unknown;
  timestamp: string;
}

export interface SessionExecutionResponse {
  session_id: string;
  events: ExecutionEvent[];
}
