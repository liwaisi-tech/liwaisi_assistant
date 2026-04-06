export type HITLAction = 'approve' | 'reject' | 'revise';

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
}
