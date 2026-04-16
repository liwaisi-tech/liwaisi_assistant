import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  ApiError,
  SessionNotFoundError,
  SessionInactiveError,
  getSession,
  sendMessage,
  resolveHITL,
} from './api';

type FetchMock = ReturnType<typeof vi.fn>;

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('api error mapping (REQ-101, §4.3)', () => {
  let fetchMock: FetchMock;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe('SessionNotFoundError', () => {
    it('maps 404 + {"error":"session not found"} to SessionNotFoundError', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: 'session not found' }));
      await expect(getSession('sess_abc')).rejects.toBeInstanceOf(SessionNotFoundError);
    });

    it('maps 410 with matching body to SessionNotFoundError', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(410, { error: 'session not found' }));
      await expect(getSession('sess_gone')).rejects.toBeInstanceOf(SessionNotFoundError);
    });

    it('is case-insensitive on the body (REQ-101)', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: 'Session Not Found' }));
      await expect(getSession('sess_case')).rejects.toBeInstanceOf(SessionNotFoundError);
    });

    it('tolerates surrounding whitespace', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: '  session not found  ' }));
      await expect(getSession('sess_ws')).rejects.toBeInstanceOf(SessionNotFoundError);
    });

    it('attaches the session id from the request path', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: 'session not found' }));
      try {
        await getSession('sess_pathid');
        throw new Error('should have thrown');
      } catch (err) {
        expect(err).toBeInstanceOf(SessionNotFoundError);
        expect((err as SessionNotFoundError).sessionId).toBe('sess_pathid');
      }
    });

    it('falls back to ApiError when 404 body does NOT match the ghost signal', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(404, { error: 'route not found' }));
      const err = await getSession('x').catch((e) => e);
      expect(err).toBeInstanceOf(ApiError);
      expect(err).not.toBeInstanceOf(SessionNotFoundError);
      expect((err as ApiError).status).toBe(404);
    });
  });

  describe('SessionInactiveError', () => {
    it('maps 409 + {"error":"session inactive"} to SessionInactiveError', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: 'session inactive' }));
      await expect(
        sendMessage('sess_idle', 'hi'),
      ).rejects.toBeInstanceOf(SessionInactiveError);
    });

    it('is case-insensitive and tolerates whitespace', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: '  Session Inactive  ' }));
      await expect(
        resolveHITL('sess_idle', 't1', { action: 'approve' }),
      ).rejects.toBeInstanceOf(SessionInactiveError);
    });

    it('does NOT map 409 with a different body to SessionInactiveError', async () => {
      fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: 'conflict' }));
      const err = await sendMessage('sess', 'hi').catch((e) => e);
      expect(err).toBeInstanceOf(ApiError);
      expect(err).not.toBeInstanceOf(SessionInactiveError);
      expect((err as ApiError).status).toBe(409);
    });
  });

  describe('typed-error prototype chain', () => {
    it('SessionNotFoundError.name is stable for narrowing', () => {
      const e = new SessionNotFoundError('x');
      expect(e.name).toBe('SessionNotFoundError');
      expect(e).toBeInstanceOf(Error);
      expect(e).toBeInstanceOf(SessionNotFoundError);
    });

    it('SessionInactiveError.name is stable', () => {
      const e = new SessionInactiveError('x');
      expect(e.name).toBe('SessionInactiveError');
      expect(e).toBeInstanceOf(SessionInactiveError);
    });
  });
});
