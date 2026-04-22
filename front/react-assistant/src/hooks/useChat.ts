import { useReducer, useEffect, useCallback, useMemo, useRef } from 'react';
import type { BackendSessionState, SessionState } from '../types/api';
import type { StreamChunkData, CPNEventData, TransitionStartedPayload, TransitionCompletedPayload, ToolExecutedPayload } from '../types/sse';
import type { ChatMessage, HITLAction, ToolExecution } from '../types/chat';
import type { A2UIAction } from '../features/chat/a2ui/types';
import { getSession, sendMessage as apiSendMessage, resolveHITL as apiResolveHITL, ApiError, HITLTransitionOrphanedError } from '../services/api';
import { useSSE, type SSEConnectionState } from './useSSE';
import { A2UI_MARKER, AWAKENING_CPN_ROLE } from '../features/chat/a2ui/constants';

// A2UI_ACTION_MARKER prefixes any user message that carries a serialized
// v0.8 userAction envelope. The CPN's `t-recv-response` transition (GAP-CPN)
// detects this marker and parses the JSON tail as `{name, context}` per
// REQ-GAP-REG-002 / AC-REG-002. Keeps the existing `POST /sessions/:id/
// messages` endpoint as the single wire for "user → agent" events without
// adding a sibling REST route.
export const A2UI_ACTION_MARKER = '$$a2ui-action:';

// CurrentActivity describes what the agent is doing right now, surfaced
// by the ActivityBubble. Populated from transition_started events that
// carry a non-null display_label.
// See spec-design-agent-activity-indicator.md §3.3.
export interface CurrentActivity {
  verb: string;
  detail?: string;
  startedAt: number;
  transitionId: string;
  cpnId: string;
}

// RecentReceipt is the post-completion pill that briefly summarises the
// just-finished work (duration + cost). Auto-dismissed by the component
// after the spec's 4 s window.
export interface RecentReceipt {
  durationMs: number;
  costUsd: number;
  shownAt: number;
}

/**
 * PendingToolApproval — a first-run HITL gate for a freshly synthesized
 * tool. Emitted by the backend as a `tool_approval_request` SSE event; the
 * frontend holds a FIFO queue keyed by request_id so multiple concurrent
 * prompts render in order. Resolved when the user clicks approve/deny and
 * the POST comes back 2xx.
 */
export interface PendingToolApproval {
  requestId: string;
  preview: unknown; // A2UIPayload shape validated at render time by the renderer itself
}

export interface ChatState {
  // Tracks the session whose stream we are willing to apply. STREAM_CHUNK
  // actions whose SessionID does not match are dropped so a late chunk from
  // a previous chat cannot bleed into the current chat's DOM (REQ-302/AC-302).
  // Null means we have not yet been told about a session — chunks are still
  // accepted in that window for backwards-compat.
  sessionId: string | null;
  messages: ChatMessage[];
  sessionState: SessionState;
  error: string | null;
  /**
   * Neutral, non-error notice string surfaced above the composer. Used for
   * benign races (e.g. HITL_TRANSITION_ORPHANED: the card the user just
   * clicked was already resolved by a parallel channel). Rendered in a
   * slate/muted style — distinct from the red `error` banner.
   * REQ-007 / AC-004 / §9.4.
   */
  notice: string | null;
  currentActivity: CurrentActivity | null;
  recentReceipt: RecentReceipt | null;
  /**
   * FIFO queue of unresolved `tool_approval_request` SSE events. The UI
   * renders the head of the queue inline in the message list; resolving
   * (approve or deny via POST /tool-approvals) removes it.
   */
  pendingToolApprovals: PendingToolApproval[];
}

export type ChatAction =
  // SESSION_LOADED carries the sessionId so the reducer can begin filtering
  // STREAM_CHUNK actions for that session (REQ-302). The state field carries
  // the backend rehydration vocabulary; the reducer maps it onto the
  // reducer-internal SessionState via mapBackendStateToReducerState.
  | { type: 'SESSION_LOADED'; sessionId: string; messages: ChatMessage[]; state: BackendSessionState }
  | { type: 'STREAM_CHUNK'; data: StreamChunkData }
  | { type: 'USER_MESSAGE'; content: string; id: string }
  | { type: 'SESSION_COMPLETED' }
  | { type: 'SESSION_FAILED' }
  | { type: 'SET_SENDING' }
  | { type: 'SET_ERROR'; error: string }
  | { type: 'CLEAR_ERROR' }
  | { type: 'HITL_REQUESTED'; transitionId: string; prompt: string; cpnId: string; cpnRole: string; suppressBubble?: boolean }
  | { type: 'HITL_RESOLVED'; transitionId: string; action: HITLAction; resolvedPayload?: string }
  // HITL_ORPHANED: the backend reported HITL_TRANSITION_ORPHANED for the
  // given transitionId. Locks the surface with a stable `{"action":
  // "orphaned"}` envelope (parallels the existing `expired` sentinel) so
  // the renderer dims buttons without flashing a red error banner, and
  // raises a neutral `notice` string. §4.3 / REQ-007 / AC-004.
  | { type: 'HITL_ORPHANED'; transitionId: string; notice: string }
  | { type: 'CLEAR_NOTICE' }
  | { type: 'ACTIVITY_START'; transitionId: string; cpnId: string; sessionId: string; verb: string; detail?: string }
  | { type: 'ACTIVITY_END'; transitionId: string; sessionId: string; durationMs?: number; costUsd?: number }
  | { type: 'ACTIVITY_RECEIPT_SHOW'; durationMs: number; costUsd: number }
  | { type: 'ACTIVITY_RECEIPT_DISMISS' }
  | { type: 'INJECT_LOCAL_MESSAGE'; id: string; content: string; cpnRole?: string }
  | { type: 'UPDATE_MESSAGE_CONTENT'; id: string; content: string }
  | { type: 'TOOL_EXECUTED'; cpnId: string; sessionId: string; execution: ToolExecution }
  | { type: 'TOOL_APPROVAL_REQUESTED'; requestId: string; preview: unknown }
  | { type: 'TOOL_APPROVAL_RESOLVED'; requestId: string }
  | { type: 'RESET' };

// mapBackendStateToReducerState translates the rehydration-oriented vocabulary
// returned by GET /api/v1/sessions/{id} into the reducer-internal SessionState.
// Visual behavior is identical to a fresh 'running' state — the existing
// "thinking…" indicator (MessageList.showThinking) covers both the live and
// rehydrated cases without needing a dedicated 'generating' value.
//   running       → 'running'   (REQ-401: CPN running, indicator visible while no chunks)
//   hitl_pending  → 'idle'      (REQ-403: A2UI surface IS the affordance — no spinner)
//   terminal      → 'idle'      (REQ-404: completed/failed are terminal, composer enabled)
//   idle          → 'idle'
// See spec-process-bugfix-a2ui-rehydration-completion.md changelog 1.1
// for the rationale on collapsing the originally-proposed 'generating' state.
function mapBackendStateToReducerState(s: BackendSessionState): SessionState {
  if (s === 'running') return 'running';
  return 'idle';
}

// narrowToBackendState collapses the wire-level SessionState | BackendSessionState
// union onto the reducer's expected BackendSessionState. Any legacy/list
// vocabulary (waiting/completed/failed) is treated as 'idle' for rehydration —
// the affordance the user needs is read from the persisted message list, not
// from the legacy state value.
// enrichWithResolutions pairs each A2UI assistant row with a later HITL
// response row (linked via parentMessageId) so the questionnaire component
// can render in a locked/read-only state showing the submitted answers
// (REQ-102..105 — spec-process-bugfix-a2ui-hitl-response-persistence.md).
// For action-envelope responses ({"action":"approve|revise|reject"}), it
// also restores hitlResolved so the existing "You approved" badge and the
// hitl:* button-lock pattern reappear after rehydration. Iterating once
// over the array keeps this O(n).
export function enrichWithResolutions(
  messages: ChatMessage[],
  sessionState?: BackendSessionState,
): ChatMessage[] {
  const resolutions = new Map<string, { payload: string; at: Date; action: HITLAction | null }>();
  for (const m of messages) {
    if (m.role === 'user' && m.parentMessageId) {
      resolutions.set(m.parentMessageId, {
        payload: m.content,
        at: m.timestamp,
        action: extractEnvelopeAction(m.content),
      });
    }
  }

  // FIX-HITL-PERSIST: Detect A2UI surfaces that have no paired response row
  // AND the session is not in an active HITL-waiting state. These are "stale"
  // surfaces from a previous CPN run that was interrupted (user logged out
  // while HITL was pending). Mark them as expired so the renderer disables
  // the buttons instead of showing an interactive form that can't be resolved.
  const isHITLActive = sessionState === 'hitl_pending';

  const result: ChatMessage[] = [];
  for (const m of messages) {
    // Drop HITL response rows — their content is surfaced via the parent
    // A2UI row's resolvedPayload, not as a standalone user bubble.
    if (m.role === 'user' && m.parentMessageId) continue;

    // Check if this is a resolved A2UI surface.
    const hit = resolutions.get(m.id);
    if (hit) {
      result.push({
        ...m,
        resolvedPayload: hit.payload,
        resolvedAt: hit.at,
        ...(hit.action ? { hitlResolved: hit.action } : {}),
      });
      continue;
    }

    // Check if this is an unresolved A2UI surface that should be expired.
    if (
      !isHITLActive &&
      m.role === 'assistant' &&
      m.content.trimStart().startsWith(A2UI_MARKER) &&
      !resolutions.has(m.id)
    ) {
      // Set resolvedPayload to a sentinel so the renderer locks the surface.
      // Use a JSON envelope with action "expired" so buttons are dimmed
      // and the questionnaire shows a stale state. No hitlResolved badge
      // since no human action was taken.
      result.push({
        ...m,
        resolvedPayload: '{"action":"expired"}',
        resolvedAt: m.timestamp,
      });
      continue;
    }

    result.push(m);
  }
  return result;
}

// extractEnvelopeAction returns the HITL action keyword embedded in an
// action-envelope response ({"action":"approve|revise|reject|submit"}).
// Returns null for questionnaire answer maps (flat or wrapped), for
// malformed JSON, and for any value that is not one of the known actions.
function extractEnvelopeAction(content: string): HITLAction | null {
  try {
    const parsed = JSON.parse(content) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
    const a = (parsed as Record<string, unknown>).action;
    if (a === 'approve' || a === 'revise' || a === 'reject' || a === 'submit') {
      return a;
    }
  } catch {
    // fall through
  }
  return null;
}

function narrowToBackendState(s: SessionState | BackendSessionState): BackendSessionState {
  if (s === 'running' || s === 'hitl_pending' || s === 'terminal' || s === 'idle') {
    return s;
  }
  return 'idle';
}

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SESSION_LOADED':
      return {
        ...state,
        sessionId: action.sessionId,
        // REQ-303: rehydrated messages MUST never carry a stale streaming
        // flag into the DOM. Force isStreaming=false on every entry.
        messages: action.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
        sessionState: mapBackendStateToReducerState(action.state),
        error: null,
      };

    case 'USER_MESSAGE':
      return {
        ...state,
        messages: [
          ...state.messages,
          {
            id: action.id,
            role: 'user',
            content: action.content,
            isStreaming: false,
            timestamp: new Date(),
          },
        ],
        error: null,
      };

    case 'SET_SENDING':
      return { ...state, sessionState: 'running' };

    case 'STREAM_CHUNK': {
      const { data } = action;

      // 0. Session-id race defence (REQ-302 / AC-302). Drop chunks that
      // belong to a session other than the one the user is viewing. This
      // closes the window between chat-switch dispatch and SSE abort. The
      // null-sessionId branch keeps backwards-compat for callers that
      // dispatch chunks before SESSION_LOADED has set the id.
      if (state.sessionId !== null && data.SessionID !== state.sessionId) {
        return state;
      }

      // respondingModel is surfaced on the FINAL chunk (Done=true) of an
      // LLM transition per REQ-GAP-IND-001. Support both snake_case (wire)
      // and PascalCase (legacy reducer field) so the reducer tolerates
      // either shape without a breaking migration. Empty string is
      // treated as absent so older bubbles rehydrate without a badge.
      const respondingModel =
        (typeof data.responding_model === 'string' && data.responding_model.length > 0
          ? data.responding_model
          : undefined) ??
        (typeof data.ResponsibleModel === 'string' && data.ResponsibleModel.length > 0
          ? data.ResponsibleModel
          : undefined);

      // 1. Done sentinel — no content, just signals response complete.
      // When the final chunk carries a respondingModel, stamp it onto the
      // most-recent streaming assistant bubble for this CPNID before
      // closing it (REQ-GAP-IND-003).
      if (data.Done && !data.Content) {
        return {
          ...state,
          sessionState: 'idle',
          messages: state.messages.map((m) => {
            if (!m.isStreaming) return m;
            if (respondingModel && m.cpnId === data.CPNID && m.role === 'assistant') {
              return {
                ...m,
                isStreaming: false,
                metadata: { ...m.metadata, responding_model: respondingModel },
              };
            }
            return { ...m, isStreaming: false };
          }),
        };
      }

      const newBubble = (): ChatMessage => ({
        id: `assistant-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        role: 'assistant',
        content: data.Content,
        isStreaming: !data.Done,
        cpnId: data.CPNID,
        cpnRole: data.CPNRole,
        timestamp: new Date(),
        ...(data.Done && respondingModel
          ? { metadata: { responding_model: respondingModel } }
          : {}),
      });

      // 2. A2UI boundary (REQ-008/009): any chunk starting with the marker
      // opens a brand-new assistant bubble. Every currently-streaming
      // assistant message is closed first so a mid-flight LLM bubble does
      // not swallow the marker.
      if (data.Content.startsWith(A2UI_MARKER)) {
        return {
          ...state,
          sessionState: data.Done ? 'idle' : 'running',
          messages: [
            ...state.messages.map((m) =>
              m.role === 'assistant' && m.isStreaming
                ? { ...m, isStreaming: false }
                : m
            ),
            newBubble(),
          ],
        };
      }

      // 3. Post-A2UI guard (REQ-010): if a streaming assistant bubble on the
      // same CPNID is already holding a complete A2UI payload, a follow-on
      // non-marker chunk MUST NOT be concatenated onto it. Close the A2UI
      // bubble and start a fresh plain bubble.
      const a2uiIdx = state.messages.findIndex(
        (m) =>
          m.role === 'assistant' &&
          m.isStreaming &&
          m.cpnId === data.CPNID &&
          m.content.startsWith(A2UI_MARKER)
      );
      if (a2uiIdx >= 0) {
        const updated = [...state.messages];
        updated[a2uiIdx] = { ...updated[a2uiIdx], isStreaming: false };
        updated.push(newBubble());
        return {
          ...state,
          messages: updated,
          sessionState: data.Done ? 'idle' : 'running',
        };
      }

      // 4. Default append — existing behaviour.
      const existingIdx = state.messages.findIndex(
        (m) => m.role === 'assistant' && m.isStreaming && m.cpnId === data.CPNID
      );

      if (existingIdx >= 0) {
        const updated = [...state.messages];
        const prev = updated[existingIdx];
        updated[existingIdx] = {
          ...prev,
          content: prev.content + data.Content,
          isStreaming: !data.Done,
          // On the final chunk of an LLM transition, persist respondingModel
          // onto the bubble's metadata so MessageBubble can render the
          // RoundBadge without re-reading the SSE stream (REQ-GAP-IND-003).
          ...(data.Done && respondingModel
            ? { metadata: { ...prev.metadata, responding_model: respondingModel } }
            : {}),
        };
        return { ...state, messages: updated, sessionState: data.Done ? 'idle' : 'running' };
      }

      return {
        ...state,
        messages: [...state.messages, newBubble()],
        sessionState: data.Done ? 'idle' : 'running',
      };
    }

    case 'SESSION_COMPLETED':
      return {
        ...state,
        sessionState: 'completed',
        currentActivity: null,
        messages: state.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
      };

    case 'SESSION_FAILED':
      return {
        ...state,
        sessionState: 'idle',
        currentActivity: null,
        messages: state.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
      };

    case 'SET_ERROR':
      return { ...state, error: action.error };

    case 'CLEAR_ERROR':
      return { ...state, error: null };

    case 'HITL_REQUESTED': {
      // The A2UI card / HITL prompt mounts in this dispatch, becoming the
      // user-facing affordance. Clear any "Waiting for you" activity bubble
      // so the user does not see a duplicate signal (REQ-032).
      if (!action.suppressBubble) {
        return {
          ...state,
          sessionState: 'waiting',
          currentActivity: null,
          messages: [
            ...state.messages,
            {
              id: `hitl-${action.transitionId}-${Date.now()}`,
              role: 'assistant',
              content: action.prompt,
              isStreaming: false,
              cpnId: action.cpnId,
              cpnRole: action.cpnRole,
              timestamp: new Date(),
              hitlTransitionId: action.transitionId,
              hitlActions: ['approve', 'reject'],
            },
          ],
        };
      }
      // Custom-surface path: the transition already pushed an A2UI bubble
      // through STREAM_CHUNK before this HITL_REQUESTED arrived. Stamp the
      // transition id onto the most-recent A2UI assistant bubble for this
      // cpnId so HITL_RESOLVED can match it and propagate resolvedPayload
      // → ResolutionContext → QuestionnaireLocked. Without this stamp the
      // submit button never locks, allowing a double-submit that returns
      // 409 ErrNoHITLWaiting on the second click.
      let stampIdx = -1;
      for (let i = state.messages.length - 1; i >= 0; i--) {
        const m = state.messages[i];
        if (
          m.role === 'assistant' &&
          m.cpnId === action.cpnId &&
          m.content.startsWith(A2UI_MARKER) &&
          !m.hitlTransitionId
        ) {
          stampIdx = i;
          break;
        }
      }
      if (stampIdx === -1) {
        return { ...state, sessionState: 'waiting', currentActivity: null };
      }
      const stamped = [...state.messages];
      stamped[stampIdx] = {
        ...stamped[stampIdx],
        hitlTransitionId: action.transitionId,
      };
      return { ...state, sessionState: 'waiting', currentActivity: null, messages: stamped };
    }

    case 'HITL_ORPHANED': {
      // Lock every stale surface keyed by this transition. We also sweep
      // surfaces that carry a live `hitlActions` bag (no transitionId yet)
      // for the same parentage so legacy pre-fix `t-review` rows get the
      // same obsolete treatment the new host-approval cards use (CON-002).
      const orphaned = '{"action":"orphaned"}';
      const next = state.messages.map((m) => {
        if (m.hitlTransitionId !== action.transitionId) return m;
        return {
          ...m,
          hitlActions: undefined,
          resolvedPayload: orphaned,
          resolvedAt: new Date(),
        };
      });
      return {
        ...state,
        // Clear the red error banner: this is a benign race, not a failure.
        error: null,
        notice: action.notice,
        sessionState: 'idle',
        messages: next,
      };
    }

    case 'CLEAR_NOTICE':
      return { ...state, notice: null };

    case 'HITL_RESOLVED':
      return {
        ...state,
        sessionState: 'running',
        messages: state.messages.map((m) =>
          m.hitlTransitionId === action.transitionId
            ? {
                ...m,
                hitlResolved: action.action,
                hitlActions: undefined,
                // Lock immediately on live submission so the questionnaire
                // renders read-only without waiting for a reload (REQ-105).
                ...(action.resolvedPayload
                  ? { resolvedPayload: action.resolvedPayload, resolvedAt: new Date() }
                  : {}),
              }
            : m
        ),
      };

    case 'ACTIVITY_START': {
      // Session-id race guard (REQ-020). Drop events for sessions other
      // than the one the user is viewing. The null branch keeps backward
      // compat with chunk dispatch (parity with STREAM_CHUNK rule).
      if (state.sessionId !== null && action.sessionId !== state.sessionId) {
        return state;
      }
      // Idempotence (REQ-016). Re-dispatch with the same transitionId is
      // a no-op so duplicate transition_started events do not reset the
      // visible startedAt clock.
      if (state.currentActivity?.transitionId === action.transitionId) {
        return state;
      }
      return {
        ...state,
        currentActivity: {
          verb: action.verb,
          detail: action.detail,
          transitionId: action.transitionId,
          cpnId: action.cpnId,
          startedAt: Date.now(),
        },
      };
    }

    case 'ACTIVITY_END': {
      if (state.sessionId !== null && action.sessionId !== state.sessionId) {
        return state;
      }
      const hasReceipt = action.durationMs !== undefined;
      return {
        ...state,
        currentActivity: null,
        recentReceipt: hasReceipt
          ? {
              durationMs: action.durationMs!,
              costUsd: action.costUsd ?? 0,
              shownAt: Date.now(),
            }
          : state.recentReceipt,
      };
    }

    case 'ACTIVITY_RECEIPT_SHOW':
      return {
        ...state,
        recentReceipt: {
          durationMs: action.durationMs,
          costUsd: action.costUsd,
          shownAt: Date.now(),
        },
      };

    case 'ACTIVITY_RECEIPT_DISMISS':
      return { ...state, recentReceipt: null };

    case 'INJECT_LOCAL_MESSAGE':
      return {
        ...state,
        messages: [
          ...state.messages,
          {
            id: action.id,
            role: 'assistant',
            content: action.content,
            isStreaming: false,
            cpnRole: action.cpnRole,
            timestamp: new Date(),
          },
        ],
      };

    case 'UPDATE_MESSAGE_CONTENT':
      return {
        ...state,
        messages: state.messages.map((m) =>
          m.id === action.id ? { ...m, content: action.content } : m,
        ),
      };

    case 'TOOL_EXECUTED': {
      if (state.sessionId !== null && action.sessionId !== state.sessionId) {
        return state;
      }
      // Append to the most recent assistant message for this cpnId.
      // Prefer the active streaming message; fall back to the last
      // non-streaming assistant message if the tool fired after Done.
      let targetIdx = -1;
      for (let i = state.messages.length - 1; i >= 0; i--) {
        const m = state.messages[i];
        if (m.role === 'assistant' && m.cpnId === action.cpnId) {
          targetIdx = i;
          break;
        }
      }
      if (targetIdx === -1) return state;
      const updated = [...state.messages];
      const prev = updated[targetIdx];
      updated[targetIdx] = {
        ...prev,
        toolExecutions: [...(prev.toolExecutions ?? []), action.execution],
      };
      return { ...state, messages: updated };
    }

    case 'TOOL_APPROVAL_REQUESTED': {
      // Idempotent: a duplicate request_id (e.g. SSE reconnect replay) MUST
      // not stack twice in the queue.
      if (state.pendingToolApprovals.some((p) => p.requestId === action.requestId)) {
        return state;
      }
      return {
        ...state,
        pendingToolApprovals: [
          ...state.pendingToolApprovals,
          { requestId: action.requestId, preview: action.preview },
        ],
      };
    }

    case 'TOOL_APPROVAL_RESOLVED':
      return {
        ...state,
        pendingToolApprovals: state.pendingToolApprovals.filter(
          (p) => p.requestId !== action.requestId,
        ),
      };

    case 'RESET':
      return { ...initialState };

    default:
      return state;
  }
}

export const initialState: ChatState = {
  sessionId: null,
  messages: [],
  sessionState: 'idle',
  error: null,
  notice: null,
  currentActivity: null,
  recentReceipt: null,
  pendingToolApprovals: [],
};

export interface UseChatOptions {
  onTransitionStarted?: (data: CPNEventData) => void;
  onTransitionCompleted?: (data: CPNEventData) => void;
  onSubNetStarted?: (data: CPNEventData) => void;
  onSubNetCompleted?: (data: CPNEventData) => void;
  onSubNetFailed?: (data: CPNEventData) => void;
  onSessionCompleted?: () => void;
  /**
   * Fired when SSE reveals a ghost session id (REQ-102, AC-007). The caller
   * is expected to delegate to `useChatList.recoverFromGhost` so recovery
   * runs exactly once per ghost id.
   */
  onSessionNotFound?: (sessionId: string) => void;
}

/**
 * AwakeningPhase gates the composer while the `brae-awakens` CPN topology
 * (spec-architecture-brae-awakening-self-discovery.md §4.3) drives the
 * session's first agentic turn.
 *   • `pending`  — fresh session, no assistant message has landed yet.
 *                  Composer is disabled, placeholder reads "brae is waking up…".
 *   • `complete` — either the awakening card arrived (cpnRole='awakening')
 *                  or the session already has at least one assistant row
 *                  (rehydration of an older chat). Composer is unlocked.
 * See REQ-008 / AC-001 / BEH-003.
 */
export type AwakeningPhase = 'pending' | 'complete';

/**
 * Pure awakening-phase derivation. Exported so the state machine is
 * covered by focused unit tests without driving the full reducer/hook.
 * See `useChat.test.ts > awakening phase` for the contract under test.
 */
export function computeAwakeningPhase(
  sessionId: string | null,
  messages: ChatMessage[],
  sessionState: SessionState,
): AwakeningPhase {
  if (!sessionId) return 'complete';
  for (const m of messages) {
    if (m.role === 'assistant') {
      // Explicit awakening role is the happy path; any assistant row at
      // all means the agent has spoken (either live or rehydrated) so
      // the gate opens either way.
      if (m.cpnRole === AWAKENING_CPN_ROLE) return 'complete';
      return 'complete';
    }
  }
  if (sessionState === 'running' || sessionState === 'waiting') {
    return 'pending';
  }
  return 'complete';
}

export interface UseChatReturn {
  messages: ChatMessage[];
  sessionState: SessionState;
  sessionId: string | null;
  isConnected: boolean;
  connectionState: SSEConnectionState;
  /**
   * Awakening gate — `pending` until the session's first assistant message
   * lands, then `complete`. Drives MessageInput's disabled state and its
   * "waking up" placeholder copy (REQ-008 / AC-001).
   */
  awakeningPhase: AwakeningPhase;
  sendMessage: (content: string) => Promise<void>;
  resolveHITL: (transitionId: string, action: HITLAction) => Promise<void>;
  error: string | null;
  /** Neutral informational notice (e.g. stale HITL dismissal). */
  notice: string | null;
  /** Clear the transient neutral notice. */
  clearNotice: () => void;
  currentActivity: CurrentActivity | null;
  recentReceipt: RecentReceipt | null;
  dismissReceipt: () => void;
  /**
   * Outstanding `tool_approval_request` SSE events awaiting the user's
   * approve/deny click. The chat surface renders the head of the queue
   * inline; resolving removes it (see `resolveToolApproval`).
   */
  pendingToolApprovals: PendingToolApproval[];
  resolveToolApproval: (requestId: string) => void;
  // Local message injection — used by the in-chat A2UI model-admin
  // fragment to drop a synthetic assistant bubble without a backend
  // round-trip. The bubble's content carries a `$$a2ui:` payload which
  // the existing renderer picks up.
  injectLocalMessage: (id: string, content: string, cpnRole?: string) => void;
  updateMessageContent: (id: string, content: string) => void;
  // sendUserAction serializes a v0.8 userAction envelope onto the chat
  // message wire so the CPN's recv-response transition can act on it
  // without the frontend calling /admin/models directly (REQ-FE-006 /
  // REQ-GAP-REG-002).
  sendUserAction: (action: A2UIAction) => Promise<void>;
}

export function useChat(sessionId: string | null, options?: UseChatOptions): UseChatReturn {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  const prevSessionIdRef = useRef<string | null>(null);

  // Load session messages when sessionId changes
  useEffect(() => {
    if (sessionId === prevSessionIdRef.current) return;
    prevSessionIdRef.current = sessionId;

    if (!sessionId) {
      dispatch({ type: 'RESET' });
      return;
    }

    let cancelled = false;

    async function loadSession() {
      try {
        const session = await getSession(sessionId!);
        if (cancelled) return;
        const messages: ChatMessage[] = session.messages.map((m) => ({
          id: m.id,
          role: m.role as 'user' | 'assistant',
          content: m.content,
          isStreaming: false,
          cpnId: m.cpn_id,
          timestamp: new Date(m.timestamp),
          parentMessageId: m.parent_message_id,
        }));
        // REQ-102..104: pair each A2UI surface with its HITL response row
        // (linked via parent_message_id) so the questionnaire can render
        // locked with the answers the user submitted
        // (spec-process-bugfix-a2ui-hitl-response-persistence.md).
        const backendState = narrowToBackendState(session.state);
        const enriched = enrichWithResolutions(messages, backendState);
        dispatch({
          type: 'SESSION_LOADED',
          sessionId: sessionId!,
          messages: enriched,
          // GET /sessions/{id} returns BackendSessionState (REQ-404), but the
          // shared SessionResponse interface widens it to SessionState |
          // BackendSessionState. Narrow defensively at the seam: any legacy
          // SessionState value (waiting/completed/failed) maps to 'idle' from
          // the reducer's perspective.
          state: backendState,
        });
      } catch (err) {
        if (cancelled) return;
        // New session with no messages yet is fine
        if (err instanceof ApiError && err.status === 404) {
          dispatch({ type: 'SESSION_LOADED', sessionId: sessionId!, messages: [], state: 'idle' });
        } else {
          dispatch({ type: 'SET_ERROR', error: (err as Error).message });
        }
      }
    }

    dispatch({ type: 'RESET' });
    loadSession();

    return () => { cancelled = true; };
  }, [sessionId]);

  const onStreamChunk = useCallback((data: StreamChunkData) => {
    dispatch({ type: 'STREAM_CHUNK', data });
  }, []);

  const onSessionCompleted = useCallback(() => {
    dispatch({ type: 'SESSION_COMPLETED' });
    options?.onSessionCompleted?.();
  }, [options?.onSessionCompleted]);

  const onSessionFailed = useCallback(() => {
    dispatch({ type: 'SESSION_FAILED' });
  }, []);

  const onHITLRequested = useCallback((data: CPNEventData) => {
    let planContent = '';
    let prompt = 'Please review and confirm.';
    let customSurface = false;

    if (typeof data.Payload === 'string') {
      prompt = data.Payload;
    } else if (data.Payload && typeof data.Payload === 'object') {
      const p = data.Payload as Record<string, unknown>;
      if (typeof p.prompt === 'string') prompt = p.prompt;
      if (typeof p.content === 'string') planContent = p.content;
      // Backend signals via HITLRequestedPayload.CustomSurface that the
      // transition already pushed its own A2UI surface (e.g. questionnaire)
      // through the stream_chunk path. In that case we must NOT create an
      // extra prompt bubble — the surface already contains everything the
      // user needs to see and act on.
      if (p.custom_surface === true) customSurface = true;
    }

    if (planContent) {
      dispatch({
        type: 'STREAM_CHUNK',
        data: {
          SessionID: data.SessionID,
          CPNID: data.CPNID,
          CPNRole: data.CPNRole,
          Content: planContent,
          Done: true,
        },
      });
    }

    dispatch({
      type: 'HITL_REQUESTED',
      transitionId: data.TransitionID,
      prompt,
      cpnId: data.CPNID,
      cpnRole: data.CPNRole,
      suppressBubble: customSurface,
    });
  }, []);

  // Activity-indicator wiring (REQ-017). transition_started events with a
  // non-null display_label populate currentActivity; transition_completed
  // clears it (and surfaces a receipt when totals are present). External
  // callbacks (options?.onTransitionStarted/Completed) still fire so the
  // execution monitor can subscribe independently.
  const onTransitionStarted = useCallback(
    (data: CPNEventData) => {
      const payload = data.Payload as TransitionStartedPayload | undefined;
      const label = payload?.display_label;
      if (label?.verb) {
        dispatch({
          type: 'ACTIVITY_START',
          transitionId: data.TransitionID,
          cpnId: data.CPNID,
          sessionId: data.SessionID,
          verb: label.verb,
          detail: label.detail,
        });
      }
      options?.onTransitionStarted?.(data);
    },
    [options?.onTransitionStarted]
  );

  const onTransitionCompleted = useCallback(
    (data: CPNEventData) => {
      const payload = data.Payload as TransitionCompletedPayload | undefined;
      // Only end the activity if this completion belongs to the transition
      // currently displayed — avoids ending an unrelated activity bubble
      // when bursts of completions arrive out of order.
      dispatch({
        type: 'ACTIVITY_END',
        transitionId: data.TransitionID,
        sessionId: data.SessionID,
        durationMs: payload?.duration_ms,
        costUsd: payload?.cost_usd,
      });
      options?.onTransitionCompleted?.(data);
    },
    [options?.onTransitionCompleted]
  );

  const onToolExecuted = useCallback(
    (data: CPNEventData) => {
      const raw = data.Payload as ToolExecutedPayload | undefined;
      if (!raw) return;
      // Only surface the three system-tool names from GAP-11 (REQ-020).
      const name = raw.tool_name ?? '';
      if (name !== 'bash_exec' && name !== 'file_read' && name !== 'file_write') return;
      const execution: ToolExecution = {
        id: `tool-${data.ID || Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
        toolName: name,
        namespace: raw.namespace ?? '',
        durationMs: raw.duration_ms ?? 0,
        success: raw.success ?? true,
        error: raw.error,
        arguments: raw.arguments,
        timestamp: new Date(data.Timestamp || Date.now()),
      };
      dispatch({
        type: 'TOOL_EXECUTED',
        cpnId: data.CPNID,
        sessionId: data.SessionID,
        execution,
      });
    },
    [],
  );

  const onToolApprovalRequested = useCallback(
    (data: { request_id: string; preview: unknown }) => {
      dispatch({
        type: 'TOOL_APPROVAL_REQUESTED',
        requestId: data.request_id,
        preview: data.preview,
      });
    },
    [],
  );

  const { isConnected, connectionState } = useSSE({
    sessionId,
    onStreamChunk,
    onSessionCompleted,
    onSessionFailed,
    onHITLRequested,
    onTransitionStarted,
    onTransitionCompleted,
    onToolExecuted,
    onToolApprovalRequested,
    onSubNetStarted: options?.onSubNetStarted,
    onSubNetCompleted: options?.onSubNetCompleted,
    onSubNetFailed: options?.onSubNetFailed,
    onSessionNotFound: options?.onSessionNotFound,
  });

  const resolveToolApproval = useCallback((requestId: string) => {
    dispatch({ type: 'TOOL_APPROVAL_RESOLVED', requestId });
  }, []);

  const dismissReceipt = useCallback(() => {
    dispatch({ type: 'ACTIVITY_RECEIPT_DISMISS' });
  }, []);

  const clearNotice = useCallback(() => {
    dispatch({ type: 'CLEAR_NOTICE' });
  }, []);

  const handleResolveHITL = useCallback(
    async (transitionId: string, action: HITLAction, content?: string) => {
      if (!sessionId) return;
      // Encode resolvedPayload for the reducer using the same rules the
      // backend uses when appending the HITL response row (REQ-001/002):
      //   submit       → raw content (questionnaire answers JSON)
      //   revise       → {"action":"revise","content":"..."}
      //   approve      → {"action":"approve"} or include content if present
      //   reject       → no lock (backend does not persist a response)
      let resolvedPayload: string | undefined;
      if (action === 'submit') {
        resolvedPayload = content ?? '';
      } else if (action === 'revise') {
        resolvedPayload = JSON.stringify({ action: 'revise', content: content ?? '' });
      } else if (action === 'approve') {
        resolvedPayload = content
          ? JSON.stringify({ action: 'approve', content })
          : JSON.stringify({ action: 'approve' });
      }
      dispatch({ type: 'HITL_RESOLVED', transitionId, action, resolvedPayload });
      try {
        await apiResolveHITL(sessionId, transitionId, {
          action,
          ...((action === 'revise' || action === 'submit') && content ? { content } : {}),
        });
      } catch (err) {
        // REQ-007 / AC-004: a HITL_TRANSITION_ORPHANED response is a benign
        // race (another tab or parallel channel already resolved the gate,
        // or the rehydrated card has no live backing). Dismiss the stale
        // card with a neutral notice instead of a red "Failed to respond"
        // banner.
        if (err instanceof HITLTransitionOrphanedError) {
          dispatch({
            type: 'HITL_ORPHANED',
            transitionId: err.transitionId ?? transitionId,
            notice: 'Esta aprobación ya fue resuelta',
          });
          return;
        }
        dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to respond' });
      }
    },
    [sessionId]
  );

  const sendMessage = useCallback(
    async (content: string) => {
      if (!content.trim() || !sessionId) return;

      const trimmed = content.trim();
      const id = `user-${Date.now()}`;

      dispatch({ type: 'USER_MESSAGE', content: trimmed, id });
      dispatch({ type: 'SET_SENDING' });

      try {
        await apiSendMessage(sessionId, trimmed);
      } catch (err) {
        if (err instanceof ApiError) {
          dispatch({ type: 'SET_ERROR', error: err.message });
        } else {
          dispatch({ type: 'SET_ERROR', error: 'Failed to send message' });
        }
      }
    },
    [sessionId]
  );

  const injectLocalMessage = useCallback(
    (id: string, content: string, cpnRole?: string) => {
      dispatch({ type: 'INJECT_LOCAL_MESSAGE', id, content, cpnRole });
    },
    [],
  );

  const updateMessageContent = useCallback(
    (id: string, content: string) => {
      dispatch({ type: 'UPDATE_MESSAGE_CONTENT', id, content });
    },
    [],
  );

  const sendUserAction = useCallback(
    async (action: A2UIAction) => {
      if (!sessionId) return;
      // Envelope: { name, componentId, context, payload } — keeps the
      // action name hoisted so the backend parser can switch on it
      // without having to re-parse a nested object. `context` mirrors the
      // a2ui v0.8 shape used in §4.6.2 (array of {key, value} pairs).
      const rawPayload = (action.payload ?? null) as
        | { context?: Array<{ key: string; value: unknown }>; fields?: Record<string, unknown> }
        | null;
      const envelope = {
        name: action.type,
        componentId: action.componentId,
        context: rawPayload?.context ?? [],
        fields: rawPayload?.fields ?? null,
      };
      const content = `${A2UI_ACTION_MARKER}${JSON.stringify(envelope)}`;
      try {
        await apiSendMessage(sessionId, content);
      } catch (err) {
        if (err instanceof ApiError) {
          dispatch({ type: 'SET_ERROR', error: err.message });
        } else {
          dispatch({ type: 'SET_ERROR', error: 'Failed to send action' });
        }
      }
    },
    [sessionId],
  );

  // Awakening gate (spec §4.4 / REQ-008 / AC-001). Derived from the
  // message list + session state so no extra reducer action is required:
  // the moment a STREAM_CHUNK lands that introduces an assistant message
  // — especially one stamped with cpnRole='awakening' — we flip to
  // 'complete'. `useMemo` keeps the derivation cheap and stable across
  // unrelated re-renders (vercel rerender-derived-state-no-effect).
  const awakeningPhase: AwakeningPhase = useMemo(
    () => computeAwakeningPhase(sessionId, state.messages, state.sessionState),
    [sessionId, state.messages, state.sessionState],
  );

  return {
    messages: state.messages,
    sessionState: state.sessionState,
    sessionId,
    isConnected,
    connectionState,
    awakeningPhase,
    sendMessage,
    resolveHITL: handleResolveHITL,
    error: state.error,
    notice: state.notice,
    clearNotice,
    currentActivity: state.currentActivity,
    recentReceipt: state.recentReceipt,
    dismissReceipt,
    injectLocalMessage,
    updateMessageContent,
    sendUserAction,
    pendingToolApprovals: state.pendingToolApprovals,
    resolveToolApproval,
  };
}
