/**
 * A2UI protocol constants shared across the frontend.
 *
 * The marker is an implicit chunk boundary in the SSE stream: any
 * `stream_chunk.Content` that starts with it opens a brand-new assistant
 * bubble whose remainder is a JSON-encoded `A2UIPayload`. See
 * `spec-process-bugfix-a2ui-hitl-rehydration.md` §4.1/§4.2 and
 * `spec-architecture-a2a-a2ui-protocol-integration.md` §4.6.
 */
export const A2UI_MARKER = '$$a2ui:';
