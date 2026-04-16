export interface StreamChunkData {
  SessionID: string;
  CPNID: string;
  CPNRole: string;
  Content: string;
  Done: boolean;
}

export interface CPNEventData {
  ID: string;
  Type: string;
  SessionID: string;
  CPNID: string;
  CPNDepth: number;
  CPNRole: string;
  TransitionID: string;
  TransitionKind: string;
  Payload: unknown;
  Timestamp: string;
}

export type SSEEventType =
  | 'stream_chunk'
  | 'transition_fired'
  | 'transition_started'
  | 'transition_completed'
  | 'subnet_started'
  | 'subnet_completed'
  | 'subnet_failed'
  | 'hitl_requested'
  | 'hitl_resolved'
  | 'mode_switch'
  | 'session_completed'
  | 'session_failed'
  | 'tool_executed'
  | 'personality_loaded'
  | 'personality_modified'
  | 'conflict_detected';

// ── Monitor Payload Types ─────────────────────────────────────────────────

export interface TokenSnapshotData {
  color: string;
  payload_preview: string;
  space: string;
  origin_id: string;
  origin_kind: string;
}

export interface TransitionStartedPayload {
  input_tokens: TokenSnapshotData[];
}

export interface TransitionCompletedPayload {
  output_tokens: TokenSnapshotData[];
  cost_usd: number;
  duration_ms: number;
  error?: string;
  executed_model?: string;
}
