import { afterEach, describe, expect, it, vi } from 'vitest'
import { globalPollingInterval, globalRealtime, setGlobalRealtime } from './realtime'

afterEach(() => {
  setGlobalRealtime(false)
  vi.useRealTimers()
})

describe('global realtime mode', () => {
  it('switches all default polling to five seconds', () => {
    expect(globalPollingInterval.value).toBe(30_000)
    setGlobalRealtime(true)
    expect(globalRealtime.value).toBe(true)
    expect(globalPollingInterval.value).toBe(5_000)
  })

  it('returns to the normal interval after ten minutes', async () => {
    vi.useFakeTimers()
    setGlobalRealtime(true)
    await vi.advanceTimersByTimeAsync(10 * 60_000)
    expect(globalRealtime.value).toBe(false)
    expect(globalPollingInterval.value).toBe(30_000)
  })
})
