import { describe, expect, it } from 'vitest'
import { tunnelled } from './fetch'
import { AnalyzerApi, ApiError } from '../src/lib/analyzerApi'

const reply =
  (status: string | null, body: string, type = 'application/json'): typeof fetch =>
  async () =>
    new Response(body, {
      status: 200,
      headers: status ? { 'X-Status': status, 'Content-Type': type } : { 'Content-Type': type },
    })

describe('tunnelled fetch', () => {
  it('moves a JSON body into X-Body, base64 of its UTF-8', async () => {
    let seen: RequestInit | undefined
    const base: typeof fetch = async (_i, init) => {
      seen = init
      return new Response('{}', { headers: { 'X-Status': '200' } })
    }
    await tunnelled(base)('/api/x', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ net: '/ddr4/DQ0 µ' }),
    })
    expect(seen?.body).toBeUndefined()
    expect(seen?.method).toBe('POST')
    const h = new Headers(seen?.headers)
    expect(h.get('Content-Type')).toBe('application/json')
    const decoded = new TextDecoder().decode(Uint8Array.from(atob(h.get('X-Body')!), (c) => c.charCodeAt(0)))
    expect(JSON.parse(decoded)).toEqual({ net: '/ddr4/DQ0 µ' })
  })

  it('encodes a body larger than one slice', async () => {
    let seen: RequestInit | undefined
    const base: typeof fetch = async (_i, init) => ((seen = init), new Response(''))
    const big = 'x'.repeat(100_000)
    await tunnelled(base)('/api/x', { method: 'POST', body: big })
    expect(atob(new Headers(seen?.headers).get('X-Body')!)).toBe(big)
  })

  it('refuses a body it cannot carry rather than dropping it', async () => {
    const base: typeof fetch = async () => new Response('')
    await expect(tunnelled(base)('/api/x', { method: 'POST', body: new FormData() })).rejects.toThrow(/string or bytes/)
  })

  it('restores an error status so the API client throws with the message', async () => {
    const api = new AnalyzerApi('/api', tunnelled(reply('404', '{"error":"pcb-trace-length-analyzer: not found"}')))
    const err = await api.getSession('gone').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).isMissing).toBe(true)
    expect((err as ApiError).message).toMatch(/not found/)
  })

  it('passes a real 200 through untouched, body and all', async () => {
    const res = await tunnelled(reply('200', '{"ok":true}'))('/x')
    expect(res.status).toBe(200)
    expect(await res.json()).toEqual({ ok: true })
  })

  it('leaves a response with no X-Status alone', async () => {
    const res = await tunnelled(reply(null, 'plain', 'text/plain'))('/x')
    expect(res.status).toBe(200)
    expect(await res.text()).toBe('plain')
  })

  it('gives a 204 no body', async () => {
    const res = await tunnelled(reply('204', ''))('/x')
    expect(res.status).toBe(204)
    expect(res.body).toBeNull()
  })

  it('ignores a nonsense status rather than throwing from the Response constructor', async () => {
    const res = await tunnelled(reply('abc', '{}'))('/x')
    expect(res.status).toBe(200)
  })
})
