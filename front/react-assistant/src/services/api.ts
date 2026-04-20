import type { SessionResponse, SessionDetailResponse, StatusResponse, CreateSessionRequest, SendMessageRequest, ResolveHITLRequest, BalanceResponse, SessionListResponse, UpdateSessionRequest, ForkSessionRequest, ForkSessionResponse } from '../types/api';
import type { FlowListResponse, FlowDetail, SessionExecutionResponse } from '../types/flow';
import type { PersonalityResponse, UpdatePrincipleRequest, SetHierarchyRequest, PrincipleResponse, TensionResponse, ToolListResponse } from '../types/personality';
import type { UserProfile, UpdatePreferencesPayload, OnboardingCompleteRequest, ModelsResponse, ModelRegistryEntry } from '../types/setup';
import type { AdminConfigResponse, PlatformStatusResponse } from '../types/admin';

const BASE_URL = '/api/v1';

// Token getter function injected from AuthContext via setTokenGetter.
let tokenGetter: (() => string | null) | null = null;

export function setTokenGetter(fn: () => string | null) {
  tokenGetter = fn;
}

export function getAuthToken(): string | null {
  return tokenGetter?.() ?? null;
}

/**
 * SESSION_NOT_FOUND_RE matches backend payloads signaling a ghost session
 * (the row is gone from persistence or was soft-deleted, OR the id was wiped
 * from the in-memory map of a restarted process and could not be rehydrated).
 * Match is case-insensitive per REQ-101.
 */
const SESSION_NOT_FOUND_RE = /^\s*session not found\s*$/i;

/**
 * SESSION_INACTIVE_RE matches the 409 payload emitted by the backend when a
 * stream-only operation (SendStreamChunk/ResolveHITL) is issued against a
 * session that rehydrated successfully but has no live execution attached.
 */
const SESSION_INACTIVE_RE = /^\s*session inactive\s*$/i;

/**
 * HITL_ORPHAN_CODE is the stable machine-readable code the backend returns
 * (per spec-process-bugfix-tool-hitl-single-gate §4.3) when ResolveHITL is
 * invoked against a transition whose token has already been consumed. The
 * frontend treats it as a benign race: the stale card is dismissed with a
 * neutral toast rather than a red banner (REQ-007 / AC-004).
 */
const HITL_ORPHAN_CODE = 'HITL_TRANSITION_ORPHANED';

/** Extract the session id from a `/sessions/{id}/...` API path. Best-effort. */
function extractSessionId(path: string): string {
  const match = path.match(/\/sessions\/([^/?]+)/);
  return match ? match[1] : '';
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
    const rawBody = await response.json().catch(() => ({ error: response.statusText }));
    const errorMessage: string =
      typeof rawBody?.error === 'string'
        ? rawBody.error
        : typeof rawBody?.error?.message === 'string'
          ? rawBody.error.message
          : response.statusText;
    // The backend emits HITL_TRANSITION_ORPHANED as a structured envelope
    // `{"error":{"code":"HITL_TRANSITION_ORPHANED","message":"..."}}` with
    // HTTP 409 (§4.3). We surface it as a dedicated typed error so the
    // chat layer can dismiss the stale card without flashing a red banner.
    const errorCode: string | undefined =
      typeof rawBody?.error?.code === 'string' ? rawBody.error.code : undefined;
    if (errorCode === HITL_ORPHAN_CODE) {
      const transitionId: string | undefined =
        typeof rawBody?.error?.transitionId === 'string' ? rawBody.error.transitionId : undefined;
      throw new HITLTransitionOrphanedError(errorMessage, transitionId);
    }

    // Map typed session errors BEFORE falling back to the generic ApiError so
    // callers can catch them narrowly (GUD-003, PAT-002).
    if (response.status === 404 || response.status === 410) {
      if (SESSION_NOT_FOUND_RE.test(errorMessage)) {
        throw new SessionNotFoundError(extractSessionId(path));
      }
    }
    if (response.status === 409 && SESSION_INACTIVE_RE.test(errorMessage)) {
      throw new SessionInactiveError(extractSessionId(path));
    }

    throw new ApiError(response.status, errorMessage);
  }
  return response.json();
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

/**
 * Thrown when the backend reports that a session id no longer exists (either
 * absent from persistence, soft-deleted, or not rehydratable). Callers MUST
 * treat the associated session id as dead: drop any cached reference, clear
 * stale localStorage entries, and pick/create a fresh session. See REQ-101,
 * REQ-102, REQ-104, §4.3.
 */
export class SessionNotFoundError extends Error {
  readonly name = 'SessionNotFoundError';
  constructor(public readonly sessionId: string) {
    super(`session not found: ${sessionId}`);
    // Required so `instanceof` works after transpilation down-levels.
    Object.setPrototypeOf(this, SessionNotFoundError.prototype);
  }
}

/**
 * Thrown on a 409 from stream-only endpoints when the session exists but has
 * no live execution attached. Callers can retry against a new run without
 * abandoning the session id. See §4.3.
 */
export class SessionInactiveError extends Error {
  readonly name = 'SessionInactiveError';
  constructor(public readonly sessionId: string) {
    super(`session inactive: ${sessionId}`);
    Object.setPrototypeOf(this, SessionInactiveError.prototype);
  }
}

/**
 * Thrown on a 409 (or equivalent) from `ResolveHITL` when the target
 * transition has already been resolved or orphaned (its token was already
 * consumed, e.g. because a parallel channel fired the approval or the
 * rehydrated card is backing a long-gone flow). Callers should dismiss
 * the stale HITL surface with a neutral notice — not a red error banner
 * (REQ-007 / AC-004 / §9.4). The optional `transitionId` lets the reducer
 * lock the specific stale row even if the click arrived with a different
 * card id in focus. */
export class HITLTransitionOrphanedError extends Error {
  readonly name = 'HITLTransitionOrphanedError';
  constructor(
    message: string,
    public readonly transitionId?: string,
  ) {
    super(message);
    Object.setPrototypeOf(this, HITLTransitionOrphanedError.prototype);
  }
}

export class AdminApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = 'AdminApiError';
  }
}

async function adminRequest<T>(path: string, options?: RequestInit): Promise<T> {
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

  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new AdminApiError(response.status, body.error || response.statusText);
  }
  return response.json();
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

// ── Personality API ─────────────────────────────────────────────────────

export async function getPersonality(): Promise<PersonalityResponse> {
  return request<PersonalityResponse>('/personality');
}

export async function updatePrinciple(kind: string, data: UpdatePrincipleRequest): Promise<PersonalityResponse> {
  return request<PersonalityResponse>(`/personality/principles/${kind}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function setHierarchy(data: SetHierarchyRequest): Promise<PersonalityResponse> {
  return request<PersonalityResponse>('/personality/hierarchy', {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function resetPersonality(): Promise<PersonalityResponse> {
  return request<PersonalityResponse>('/personality/reset', {
    method: 'POST',
  });
}

export async function previewPersonality(data: { principles: PrincipleResponse[]; hierarchy: string[]; tensions: TensionResponse[]; sample_prompt: string }): Promise<{ system_prompt: string }> {
  return request<{ system_prompt: string }>('/personality/preview', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

// ── Tools API ───────────────────────────────────────────────────────────

export async function listTools(namespace?: string): Promise<ToolListResponse> {
  const params = new URLSearchParams();
  if (namespace) params.set('namespace', namespace);
  const qs = params.toString();
  return request<ToolListResponse>(`/tools${qs ? `?${qs}` : ''}`);
}

// ── Waitlist API (public, no auth) ─────────────────────────────────────

export interface WaitlistResponse {
  ok: boolean;
  message: string;
}

// ── User Profile & Onboarding API ──────────────────────────────────────

export async function getUserProfile(): Promise<UserProfile> {
  return request<UserProfile>('/user/profile');
}

export async function updatePreferences(prefs: UpdatePreferencesPayload): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/user/preferences', {
    method: 'PUT',
    body: JSON.stringify(prefs),
  });
}

export async function completeOnboarding(data: OnboardingCompleteRequest): Promise<{ ok: boolean; message: string }> {
  return request<{ ok: boolean; message: string }>('/user/onboarding/complete', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getModels(): Promise<ModelsResponse> {
  return request<ModelsResponse>('/models');
}

// ── Admin Config API ──────────────────────────────────────────────────

export async function getAdminConfig(): Promise<AdminConfigResponse> {
  return adminRequest<AdminConfigResponse>('/admin/config');
}

export async function setAdminConfig(key: string, value: string): Promise<{ ok: boolean }> {
  return adminRequest<{ ok: boolean }>(`/admin/config/${key}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
  });
}

export async function deleteAdminConfig(key: string): Promise<{ ok: boolean }> {
  return adminRequest<{ ok: boolean }>(`/admin/config/${key}`, {
    method: 'DELETE',
  });
}

export async function getPlatformStatus(): Promise<PlatformStatusResponse> {
  return request<PlatformStatusResponse>('/admin/config/status');
}

// ── Admin Model Registry API ──────────────────────────────────────────
//
// Used by the in-chat A2UI management fragment (spec-architecture-model-
// registry-and-a2ui-management.md §5 REQ-API-001..007). All mutations
// hit the backend AdminMiddleware; callers must be in LIWAISI_ADMIN_EMAILS.

export interface AdminModelListResponse {
  items: ModelRegistryEntry[];
  total: number;
  page: number;
  size: number;
}

export async function adminListModels(params?: {
  vendor?: string;
  lifecycle?: string;
  license?: string;
  invokable?: boolean;
  search?: string;
}): Promise<AdminModelListResponse> {
  const qp = new URLSearchParams();
  if (params?.vendor) qp.set('vendor', params.vendor);
  if (params?.lifecycle) qp.set('lifecycle', params.lifecycle);
  if (params?.license) qp.set('license', params.license);
  if (params?.invokable !== undefined) qp.set('invokable', String(params.invokable));
  if (params?.search) qp.set('search', params.search);
  const qs = qp.toString();
  return adminRequest<AdminModelListResponse>(`/admin/models${qs ? `?${qs}` : ''}`);
}

export async function adminSetDefaultModel(registryID: string): Promise<ModelRegistryEntry> {
  return adminRequest<ModelRegistryEntry>('/admin/models/set-default', {
    method: 'POST',
    body: JSON.stringify({ registry_id: registryID }),
  });
}

export async function adminLicenseReview(
  registryID: string,
  status: string,
  note?: string,
): Promise<ModelRegistryEntry> {
  return adminRequest<ModelRegistryEntry>('/admin/models/license-review', {
    method: 'POST',
    body: JSON.stringify({ registry_id: registryID, status, ...(note ? { note } : {}) }),
  });
}

export async function adminDeleteModel(registryID: string): Promise<{ ok: boolean; registry_id: string }> {
  return adminRequest<{ ok: boolean; registry_id: string }>(`/admin/models/${encodeURIComponent(registryID)}`, {
    method: 'DELETE',
  });
}

export async function joinWaitlist(email: string): Promise<WaitlistResponse> {
  const response = await fetch(`${BASE_URL}/waitlist`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new ApiError(response.status, error.error || response.statusText);
  }
  return response.json();
}
