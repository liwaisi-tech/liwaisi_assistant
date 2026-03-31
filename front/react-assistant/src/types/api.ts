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
  action: 'approve' | 'reject' | 'revise';
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
