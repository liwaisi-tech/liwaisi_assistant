export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  isStreaming: boolean;
  cpnId?: string;
  cpnRole?: string;
  timestamp: Date;
}
