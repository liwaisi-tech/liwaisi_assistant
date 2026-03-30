export interface StreamChunkData {
  SessionID: string;
  CPNID: string;
  CPNRole: string;
  Content: string;
  Done: boolean;
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
  | 'subnet_started'
  | 'subnet_completed'
  | 'subnet_failed'
  | 'hitl_requested'
  | 'hitl_resolved'
  | 'mode_switch'
  | 'session_completed'
  | 'session_failed';
