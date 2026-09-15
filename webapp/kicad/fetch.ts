/**
 * fetch, adapted to the plugin's URL scheme in both directions.
 *
 * The plugin answers this page through Qt's URL scheme handler, and two things
 * that plain HTTP has do not survive it:
 *
 * - A reply can only be 200. The real status arrives in an X-Status header and
 *   is put back here. Without it the API client would read every refusal -- a
 *   session that has gone, a parameter out of range -- as a success, and try to
 *   render an error message as a report.
 * - A request body cannot be read on the plugin's side: PySide6 6.10 crashes
 *   the process on QWebEngineUrlRequestJob.requestBody(). Headers arrive
 *   intact (8 MB measured), so the body goes as base64 in X-Body instead. The
 *   bodies this page sends are a few kilobytes of JSON.
 */
export function tunnelled(base: typeof fetch = (...a) => fetch(...a)): typeof fetch {
  return async (input, init) => {
    const res = await base(input, withBodyInHeader(init))
    const raw = res.headers.get('X-Status')
    const status = raw ? Number(raw) : NaN
    if (!Number.isInteger(status) || status < 200 || status > 599 || status === res.status) {
      return res
    }
    const headers = new Headers(res.headers)
    headers.delete('X-Status')
    // A null-body status may not be given a body, even an empty one.
    const body = status === 204 || status === 205 || status === 304 ? null : await res.arrayBuffer()
    return new Response(body, { status, headers })
  }
}

function withBodyInHeader(init: RequestInit | undefined): RequestInit | undefined {
  if (!init || init.body === undefined || init.body === null) return init
  let bytes: Uint8Array
  const body = init.body
  if (typeof body === 'string') bytes = new TextEncoder().encode(body)
  else if (body instanceof ArrayBuffer) bytes = new Uint8Array(body)
  else if (body instanceof Uint8Array) bytes = body
  else {
    // FormData, Blob and streams would need reading asynchronously and
    // re-encoding; nothing inside the plugin sends them. Fail loudly rather
    // than send a request without its body.
    throw new Error('inside KiCad a request body must be a string or bytes')
  }
  const headers = new Headers(init.headers)
  headers.set('X-Body', toBase64(bytes))
  const { body: _dropped, ...rest } = init
  void _dropped
  return { ...rest, headers }
}

function toBase64(bytes: Uint8Array): string {
  let bin = ''
  // In slices: String.fromCharCode with a very long argument list overflows the stack.
  for (let i = 0; i < bytes.length; i += 0x8000) {
    bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(bin)
}
