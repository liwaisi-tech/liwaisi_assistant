export type HITLAction = 'approve' | 'reject' | 'revise' | 'submit';

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  isStreaming: boolean;
  cpnId?: string;
  cpnRole?: string;
  timestamp: Date;
  hitlTransitionId?: string;
  hitlActions?: HITLAction[];
  hitlResolved?: HITLAction;
  // parentMessageId links a HITL response row back to the A2UI surface row
  // it answers. When the reducer enriches an A2UI message with a matching
  // response it attaches resolvedPayload + resolvedAt so the questionnaire
  // can render in a read-only/locked state (REQ-102..105, REQ-201).
  parentMessageId?: string;
  resolvedPayload?: string;
  resolvedAt?: Date;
  /**
   * Optional bag of transport-level metadata surfaced from SSE payloads.
   * Currently the only key is `responding_model` — set by useChat when the
   * final `stream_chunk` carries it (REQ-GAP-IND-003 / spec §4.3). The bag
   * is designed to grow additively without breaking older rehydrated rows:
   * every consumer MUST treat `metadata?.<key>` as possibly-undefined.
   */
  metadata?: {
    responding_model?: string;
  };
}
