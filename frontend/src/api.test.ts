import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'

describe('api client', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('returns typed JSON', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ version: '1.5.0' }), { status: 200 })))
    await expect(api<{ version: string }>('/api/v1/version')).resolves.toEqual({ version: '1.5.0' })
  })

  it('surfaces backend problem messages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { message: '备份失败' } }), { status: 500 })))
    await expect(api('/api/v1/backups')).rejects.toEqual(new ApiError('备份失败', 500))
  })
})
