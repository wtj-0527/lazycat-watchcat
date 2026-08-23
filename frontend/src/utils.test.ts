import { describe, expect, it, vi } from 'vitest'
import { ago, backupType, bytes, duration, formatNumber } from './utils'

describe('format helpers', () => {
  it('formats numbers and storage sizes', () => {
    expect(formatNumber(12.345)).toBe('12.3')
    expect(formatNumber('bad')).toBe('—')
    expect(bytes(1024 * 1024)).toBe('1.0 MiB')
    expect(duration(2 * 86400 + 3 * 3600)).toBe('2 天 3 小时')
    expect(backupType('pre-restore')).toBe('恢复前')
  })

  it('formats relative time', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-23T12:00:00Z'))
    expect(ago('2026-08-23T11:59:30Z')).toBe('30 秒前')
    expect(ago('2026-08-23T10:00:00Z')).toBe('2 小时前')
    vi.useRealTimers()
  })
})

import type { Device } from './types'
import { deviceState, metricValueAny } from './utils'

const baseDevice: Device = {
  id: 'd1', name: 'device', hostname: 'device', osVersion: '', collectorVersion: '', status: 'active',
  lastSeenAt: new Date().toISOString(), online: true, stale: false, health: 'healthy', latest: {},
}

describe('production state helpers', () => {
  it('does not present offline or stale devices as healthy', () => {
    expect(deviceState({ ...baseDevice, online: false })).toBe('offline')
    expect(deviceState({ ...baseDevice, stale: true })).toBe('stale')
  })

  it('uses an explicit unknown value when no metric exists', () => {
    expect(metricValueAny(baseDevice, ['missing.metric'])).toBe('未知')
  })
})
