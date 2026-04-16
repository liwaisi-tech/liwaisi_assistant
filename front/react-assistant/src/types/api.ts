// Frontend reducer-internal session state. Drives composer + loading UI.
export type SessionState = 'idle' | 'running' | 'waiting' | 'completed' | 'failed';

// Backend-side session state returned by GET /api/v1/sessions/{id} per
// spec-process-bugfix-a2ui-rehydration-completion.md REQ-404. The reducer
// maps these to SessionState at the SESSION_LOADED boundary; UI code that
// renders Record<SessionState, T> tables MUST NOT see these values.
//   running       → mapped to 'running' (composer disabled, "thinking…" affordance)
//   hitl_pending  → mapped to 'idle'    (the A2UI surface IS the affordance)
//   terminal      → mapped to 'idle'    (run done; composer enabled)
//   idle          → mapped to 'idle'
export type BackendSessionState = 'idle' | 'running' | 'hitl_pending' | 'terminal';

export interface SessionResponse {
  id: string;
  user_id: string;
  channel: string;
  // SessionResponse is shared by multiple endpoints. The /sessions/{id}
  // detail endpoint returns BackendSessionState (REQ-404); other endpoints
  // (create, list) historically return SessionState. The widened union
  // tolerates both at the wire layer; consumers map at their boundary.
  state: SessionState | BackendSessionState;
  created_at: string;
}

export interface MessageResponse {
  id: string;
  role: 'user' | 'assistant' | 'observer';
  content: string;
  cpn_id?: string;
  timestamp: string;
  // parent_message_id pairs a HITL response row with the A2UI surface it
  // answers so the reducer can lock the questionnaire on rehydration
  // (REQ-101 — spec-process-bugfix-a2ui-hitl-response-persistence.md).
  parent_message_id?: string;
}

export interface SessionDetailResponse extends SessionResponse {
  messages: MessageResponse[];
}

export interface CreateSessionRequest {
  user_id: string;
  channel: 'web' | 'whatsapp' | 'telegram';
}

export interface SendMessageRequest {
  content: string;
}

export interface ResolveHITLRequest {
  action: 'approve' | 'reject' | 'revise' | 'submit';
  content?: string;
}

export interface StatusResponse {
  status: string;
}

export interface ErrorResponse {
  error: string;
}

export interface BalanceResponse {
  limit_remaining: number | null;
  usage: number;
  usage_daily: number;
  usage_weekly: number;
  usage_monthly: number;
  is_free_tier: boolean;
}

export interface SessionListItem {
  id: string;
  title: string;
  // Same widening rationale as SessionResponse.state — list and detail
  // endpoints share the field but use different vocabularies.
  state: SessionState | BackendSessionState;
  last_message_preview: string;
  last_activity_at: string;
  created_at: string;
  total_cost_usd: number;
  message_count: number;
  forked_from_session_id: string;
}

export interface SessionListResponse {
  items: SessionListItem[];
  next_cursor: string;
  has_more: boolean;
}

export interface UpdateSessionRequest {
  title?: string;
  deleted?: boolean;
}

export interface ForkSessionRequest {
  message_index: number;
}

export interface ForkSessionResponse extends SessionResponse {
  title: string;
  forked_from_session_id: string;
  fork_message_count: number;
  total_cost_usd: number;
}
