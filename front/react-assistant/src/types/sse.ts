export interface StreamChunkData {
  SessionID: string;
  CPNID: string;
  CPNRole: string;
  Content: string;
  Done: boolean;
  // Populated only on the final chunk of an LLM transition (Done=true).
  // Encoded as "<registry_id> · <adapter>" (e.g. "anthropic/claude-opus-4-6 · openrouter").
  // REQ-GAP-IND-001/002, parent REQ-FE-005.
  ResponsibleModel?: string;
  responding_model?: string;
}

/**
 * StreamChunkPayload mirrors the backend SSE `stream_chunk` wire shape
 * (camelCase server → snake_case client by convention). Kept as a typed
 * interface so tests and future adapters can import the contract without
 * depending on the reducer's internal StreamChunkData variant.
 * REQ-GAP-IND-002 / parent §4.2 of the gap-closure spec.
 */
export interface StreamChunkPayload {
  message_id: string;
  content: string;
  done: boolean;
  // Present only on the final chunk of an LLM transition; absent on every
  // other chunk and on messages that pre-date this field.
  responding_model?: string;
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
  | 'conflict_detected'
  | 'subagent_started'
  | 'subagent_finished';

// Sub-agent lifecycle payloads. Mirror back/go-assistant/cpn/event.go
// SubAgentStartedPayload / SubAgentFinishedPayload.
export interface SubAgentStartedPayload {
  profile_id: string;
  action_id: string;
  subagent_label?: string;
  icon_key?: string;
  firing_id: string;
  model?: string;
}

export interface SubAgentFinishedPayload {
  profile_id: string;
  action_id: string;
  subagent_label?: string;
  icon_key?: string;
  firing_id: string;
  model?: string;
  duration_ms: number;
  ok: boolean;
  stage?: 'validate' | 'gate' | string;
  error?: string;
  cost_usd: number;
}

// ── Tool Executed Payload ─────────────────────────────────────────────────
// Mirrors back/go-assistant/cpn/event.go ToolExecutedPayload (REQ-FE-001).
// `arguments` is the raw JSON the LLM passed to the tool — consumers may
// parse it further for display (e.g. extracting `command` for bash_exec).

export interface ToolExecutedPayload {
  tool_name: string;
  namespace: string;
  duration_ms: number;
  success: boolean;
  error?: string;
  arguments?: Record<string, unknown>;
}

// ── Monitor Payload Types ─────────────────────────────────────────────────

export interface TokenSnapshotData {
  color: string;
  payload_preview: string;
  space: string;
  origin_id: string;
  origin_kind: string;
}

// DisplayLabel is the user-facing description of what a transition is
// doing, attached to transition lifecycle events by the backend so the
// activity indicator can render a verb (e.g. "Thinking", "Reading") with
// an optional detail (tool name, file path, sub-net role).
//
// See spec-design-agent-activity-indicator.md §4.1 for the wire contract.
export interface DisplayLabel {
  verb: string;
  detail?: string;
}

export interface TransitionStartedPayload {
  input_tokens: TokenSnapshotData[];
  display_label?: DisplayLabel;
}

export interface TransitionCompletedPayload {
  output_tokens: TokenSnapshotData[];
  cost_usd: number;
  duration_ms: number;
  error?: string;
  executed_model?: string;
  display_label?: DisplayLabel;
}
