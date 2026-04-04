import type { SessionResponse, SessionDetailResponse, StatusResponse, CreateSessionRequest, SendMessageRequest, ResolveHITLRequest, BalanceResponse, SessionListResponse, UpdateSessionRequest, ForkSessionRequest, ForkSessionResponse } from '../types/api';
import type { FlowListResponse, FlowDetail, SessionExecutionResponse } from '../types/flow';

const BASE_URL = '/api/v1';

// Token getter function injected from AuthContext via setTokenGetter.
let tokenGetter: (() => string | null) | null = null;

export function setTokenGetter(fn: () => string | null) {
  tokenGetter = fn;
}

export function getAuthToken(): string | null {
  return tokenGetter?.() ?? null;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };

  const authToken = tokenGetter?.();
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await fetch(`${BASE_URL}${path}`, {
    headers,
    ...options,
  });

  if (response.status === 401) {
    window.dispatchEvent(new Event('auth:expired'));
    throw new ApiError(401, 'Authentication expired');
  }

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new ApiError(response.status, error.error || response.statusText);
  }
  return response.json();
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

export async function createSession(userId: string, channel: string = 'web'): Promise<SessionResponse> {
  return request<SessionResponse>('/sessions', {
    method: 'POST',
    body: JSON.stringify({ user_id: userId, channel } as CreateSessionRequest),
  });
}

export async function getSession(sessionId: string): Promise<SessionDetailResponse> {
  return request<SessionDetailResponse>(`/sessions/${sessionId}`);
}

export async function sendMessage(sessionId: string, content: string): Promise<StatusResponse> {
  return request<StatusResponse>(`/sessions/${sessionId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content } as SendMessageRequest),
  });
}

export async function resolveHITL(sessionId: string, transitionId: string, req: ResolveHITLRequest): Promise<StatusResponse> {
  return request<StatusResponse>(`/sessions/${sessionId}/hitl/${transitionId}`, {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function deleteSession(sessionId: string): Promise<StatusResponse> {
  return request<StatusResponse>(`/sessions/${sessionId}`, {
    method: 'DELETE',
  });
}

export async function healthCheck(): Promise<{ status: string }> {
  return request<{ status: string }>('/health');
}

export async function getBalance(): Promise<BalanceResponse> {
  return request<BalanceResponse>('/billing/balance');
}

// ── Flow & Execution API ─────────────────────────────────────────────────

export async function getFlows(): Promise<FlowListResponse> {
  return request<FlowListResponse>('/flows');
}

export async function getFlow(hash: string): Promise<FlowDetail> {
  return request<FlowDetail>(`/flows/${hash}`);
}

export async function getSessionExecution(sessionId: string): Promise<SessionExecutionResponse> {
  return request<SessionExecutionResponse>(`/sessions/${sessionId}/execution`);
}

// ── Session Management API ──────────────────────────────────────────────────

export async function listSessions(cursor?: string, limit?: number): Promise<SessionListResponse> {
  const params = new URLSearchParams();
  if (cursor) params.set('cursor', cursor);
  if (limit) params.set('limit', String(limit));
  const qs = params.toString();
  return request<SessionListResponse>(`/sessions${qs ? `?${qs}` : ''}`);
}

export async function updateSession(sessionId: string, req: UpdateSessionRequest): Promise<StatusResponse> {
  return request<StatusResponse>(`/sessions/${sessionId}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
  });
}

export async function forkSession(sessionId: string, req: ForkSessionRequest): Promise<ForkSessionResponse> {
  return request<ForkSessionResponse>(`/sessions/${sessionId}/fork`, {
    method: 'POST',
    body: JSON.stringify(req),
  });
}
