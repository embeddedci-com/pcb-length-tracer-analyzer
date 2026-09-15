import { describe, expect, it, vi } from 'vitest'
import { AnalyzerApi } from './analyzerApi'

// Mounted in embeddedci-server, the session is a bearer token rather than a
// cookie. Every request has to carry it -- including the ones that are not
// JSON, which is where a plain link or a bare fetch used to leave it behind.
describe('AnalyzerApi credentials', () => {
  const ok = (body: BodyInit, headers: Record<string, string> = {}) =>
    new Response(body, { status: 200, headers })

  it('sends the host headers on JSON requests', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(ok(JSON.stringify({ sessions: [] })))
    const api = new AnalyzerApi('/api', fetchImpl, () => ({ Authorization: 'Bearer t' }))
    await api.listSessions()
    const init = fetchImpl.mock.calls[0][1] as RequestInit
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer t')
  })

  it('sends them on the board download too, and names the file from the response', async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(
        ok('board', { 'Content-Disposition': 'attachment; filename="b.tuned.kicad_pcb"' }),
      )
    const api = new AnalyzerApi('/api', fetchImpl, () => ({ Authorization: 'Bearer t' }))
    const got = await api.downloadResult('s1')
    expect(got.filename).toBe('b.tuned.kicad_pcb')
    const init = fetchImpl.mock.calls[0][1] as RequestInit
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer t')
  })

  it('keeps an upload multipart: the host header must not replace its content type', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(ok(JSON.stringify({ session: {}, analysis: {} })))
    const api = new AnalyzerApi('/api', fetchImpl, () => ({ Authorization: 'Bearer t' }))
    await api.upload({ board: new File(['x'], 'b.kicad_pcb') })
    const init = fetchImpl.mock.calls[0][1] as RequestInit
    expect(init.body).toBeInstanceOf(FormData)
    expect(new Headers(init.headers).get('Content-Type')).toBeNull()
  })
})
