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
}
