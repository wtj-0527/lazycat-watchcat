export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      ...options,
      signal: options.signal || AbortSignal.timeout(25_000),
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
    })
  } catch (reason) {
    if (reason instanceof DOMException && (reason.name === 'TimeoutError' || reason.name === 'AbortError')) {
      throw new ApiError('请求超时，请重试', 408)
    }
    throw reason
  }
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const body = await response.json()
      message = body.error?.message || message
    } catch {
      // Keep the HTTP fallback for non-JSON proxy/runtime errors.
    }
    throw new ApiError(message, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
