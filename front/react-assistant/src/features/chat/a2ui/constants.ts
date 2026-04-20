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

/**
 * `cpnRole` value stamped on the session's first assistant message by the
 * `brae-awakens` CPN topology (REQ-005, REQ-008). The frontend composer
 * stays locked in a "waking up" state until an assistant message bearing
 * this role lands, at which point the user may begin typing.
 * See spec-architecture-brae-awakening-self-discovery.md §4.3 / AC-001.
 */
export const AWAKENING_CPN_ROLE = 'awakening';
