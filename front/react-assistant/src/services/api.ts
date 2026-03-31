import type { SessionResponse, SessionDetailResponse, StatusResponse, CreateSessionRequest, SendMessageRequest, ResolveHITLRequest, BalanceResponse } from '../types/api';

const BASE_URL = '/api/v1';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
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

export async function healthCheck(): Promise<{ status: string }> {
  return request<{ status: string }>('/health');
}

export async function getBalance(): Promise<BalanceResponse> {
  return request<BalanceResponse>('/billing/balance');
}
