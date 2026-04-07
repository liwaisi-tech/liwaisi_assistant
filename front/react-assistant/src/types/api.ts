export type SessionState = 'idle' | 'running' | 'waiting' | 'completed' | 'failed';

export interface SessionResponse {
  id: string;
  user_id: string;
  channel: string;
  state: SessionState;
  created_at: string;
}

export interface MessageResponse {
  id: string;
  role: 'user' | 'assistant' | 'observer';
  content: string;
  cpn_id?: string;
  timestamp: string;
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
  state: SessionState;
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
