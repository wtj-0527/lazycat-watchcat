import { describe, expect, it, vi } from 'vitest'
import type { Device, Inspection, Metric } from './types'
import {
  ago,
  backupType,
  bytes,
  connectivityState,
  dateTime,
  deviceState,
  duration,
  formatMetricValue,
  formatNumber,
  inspectionState,
  metricValueAny,
  monthDay,
  parseBeijingDateTimeInput,
  storageRiskAdvice,
  storageRiskStatus,
  storageUsageMetric,
  storageUsageMetrics,
  timeOfDay,
  toBeijingDateTimeInput,
} from './utils'

const baseDevice: Device = {
  id: 'd1', name: 'device', hostname: 'device', osVersion: '', collectorVersion: '', status: 'active',
  lastSeenAt: new Date().toISOString(), online: true, stale: false, health: 'healthy', latest: {},
}
const point = (name: string, value: number, unit = 'count'): Metric => ({
  name, value, unit, labels: {}, collectedAt: new Date().toISOString(),
})
const inspection = (overrides: Partial<Inspection> = {}): Inspection => ({
  id: 'i1', triggerType: 'manual', startedAt: new Date().toISOString(), status: 'completed',
  deviceCount: 1, healthyCount: 1, warningCount: 0, criticalCount: 0, evidenceSha256: 'a'.repeat(64),
  ...overrides,
})

describe('format helpers', () => {
  it('formats numbers and binary storage sizes', () => {
    expect(formatNumber(12.345)).toBe('12.3')
    expect(formatNumber('bad')).toBe('—')
    expect(bytes(1024 * 1024)).toBe('1.0 MiB')
    expect(bytes(1024 ** 4)).toBe('1.00 TiB')
    expect(bytes(1024 ** 5)).toBe('1.00 PiB')
    expect(bytes(Number.NaN)).toBe('—')
    expect(bytes(Number.POSITIVE_INFINITY)).toBe('—')
    expect(bytes(-1)).toBe('—')
  })

  it('formats durations down to seconds', () => {
    expect(duration(2 * 86400 + 3 * 3600)).toBe('2 天 3 小时')
    expect(duration(3600 + 2 * 60)).toBe('1 小时 2 分钟')
    expect(duration(61)).toBe('1 分钟 1 秒')
    expect(duration(59)).toBe('59 秒')
    expect(duration(-1)).toBe('—')
  })

  it('formats every collector metric unit', () => {
    expect(formatMetricValue(1536, 'bytes')).toBe('1.5 KiB')
    expect(formatMetricValue(3661, 'seconds')).toBe('1 小时 1 分钟')
    expect(formatMetricValue(12.5, 'hours')).toBe('12.5 小时')
    expect(formatMetricValue(42.4, 'celsius')).toBe('42.4 ℃')
    expect(formatMetricValue(7.8, 'count')).toBe('8')
    expect(formatMetricValue(3, 'bitmask')).toBe('3')
    expect(formatMetricValue(1, 'bool')).toBe('是')
    expect(formatMetricValue(0, 'bool')).toBe('否')
    expect(formatMetricValue(85.25, '%')).toBe('85.3%')
    expect(formatMetricValue(1200.4, 'rpm')).toBe('1200 rpm')
    expect(formatMetricValue(12.3, 'widgets')).toBe('12.3 widgets')
    expect(formatMetricValue(Number.NaN, '%')).toBe('—')
  })

  it('formats labels and relative time', () => {
    expect(backupType('pre-restore')).toBe('恢复前')
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-23T12:00:00Z'))
    expect(ago('2026-08-23T11:59:30Z')).toBe('30 秒前')
    expect(ago('2026-08-23T10:00:00Z')).toBe('2 小时前')
    vi.useRealTimers()
  })

  it('formats and parses every absolute time as Beijing time', () => {
    expect(dateTime('2026-08-26T04:00:00Z')).toContain('12:00')
    expect(timeOfDay('2026-08-26T16:30:00Z')).toBe('00:30')
    expect(monthDay('2026-08-26T16:30:00Z')).toContain('8')
    expect(monthDay('2026-08-26T16:30:00Z')).toContain('27')
    expect(toBeijingDateTimeInput(new Date('2026-08-26T04:00:00Z'))).toBe('2026-08-26T12:00')
    expect(parseBeijingDateTimeInput('2026-08-26T12:00').toISOString()).toBe('2026-08-26T04:00:00.000Z')
  })
})

describe('production state helpers', () => {
  it('keeps connectivity separate from health', () => {
    expect(connectivityState(baseDevice)).toBe('online')
    expect(connectivityState({ ...baseDevice, stale: true })).toBe('stale')
    expect(connectivityState({ ...baseDevice, online: false })).toBe('offline')
    expect(connectivityState({ ...baseDevice, status: 'revoked' })).toBe('offline')
    expect(deviceState({ ...baseDevice, online: false })).toBe('offline')
    expect(deviceState({ ...baseDevice, stale: true })).toBe('stale')
    expect(deviceState({ ...baseDevice, stale: true, health: 'warning' })).toBe('warning')
    expect(deviceState({ ...baseDevice, stale: true, health: 'critical' })).toBe('critical')
    expect(deviceState({ ...baseDevice, online: false, health: 'critical' })).toBe('critical')
  })

  it('derives inspection result independently from execution status', () => {
    expect(inspectionState(inspection())).toBe('healthy')
    expect(inspectionState(inspection({ warningCount: 1, healthyCount: 0 }))).toBe('warning')
    expect(inspectionState(inspection({ warningCount: 1, criticalCount: 1, healthyCount: 0 }))).toBe('critical')
    expect(inspectionState(inspection({ deviceCount: 0, healthyCount: 0 }))).toBe('unknown')
    expect(inspectionState(inspection({ deviceCount: 2, healthyCount: 1 }))).toBe('unknown')
    expect(inspectionState(inspection({ status: 'failed' }))).toBe('error')
  })

  it('mirrors backend storage risk thresholds and picks the highest usage', () => {
    expect(storageRiskStatus(point('disk.temperature', 69, 'celsius'))).toBeUndefined()
    expect(storageRiskStatus(point('disk.temperature', 70, 'celsius'))).toBe('warning')
    expect(storageRiskStatus(point('disk.temperature', 80, 'celsius'))).toBe('critical')
    expect(storageRiskStatus(point('disk.nvme.media_errors', 1))).toBe('critical')
    expect(storageRiskStatus(point('disk.nvme.critical_warning', 1, 'bitmask'))).toBe('critical')
    expect(storageRiskStatus(point('disk.ata.reallocated_sectors', 1))).toBe('warning')
    expect(storageRiskStatus(point('disk.ata.pending_sectors', 1))).toBe('critical')
    expect(storageRiskStatus(point('disk.ata.offline_uncorrectable', 1))).toBe('critical')
    expect(storageRiskStatus(point('disk.ata.reported_uncorrectable', 1))).toBe('critical')
    expect(storageRiskAdvice(point('disk.ata.reallocated_sectors', 1))).toContain('坏扇区')

    const root = point('filesystem.root.usage', 20, '%')
    const btrfsSafe = point('btrfs.usage', 30, '%')
    const btrfsRisk = point('btrfs.usage', 96, '%')
    btrfsSafe.labels = { mount: '/volume1' }
    btrfsRisk.labels = { mount: '/volume2' }
    const device = {
      ...baseDevice,
      latest: { [root.name]: [root], [btrfsSafe.name]: [btrfsSafe, btrfsRisk] },
    }
    expect(storageUsageMetric(device)).toBe(btrfsRisk)
  })

  it('returns every storage volume and removes duplicate snapshots by mount', () => {
    const older = point('btrfs.usage', 50, '%')
    older.labels = { mount: '/data' }
    older.collectedAt = '2026-08-25T10:00:00Z'
    const data = point('btrfs.usage', 51, '%')
    data.labels = { mount: '/data', backing_device: '/dev/sda1' }
    data.collectedAt = '2026-08-25T10:05:00Z'
    const backup = point('btrfs.usage', 20, '%')
    backup.labels = { mount: '/backup' }
    backup.collectedAt = '2026-08-25T10:05:00Z'
    const device = { ...baseDevice, latest: { 'btrfs.usage': [older, data, backup] } }

    expect(storageUsageMetrics(device)).toEqual([data, backup])
  })

  it('uses an explicit unknown value when no metric exists', () => {
    expect(metricValueAny(baseDevice, ['missing.metric'])).toBe('未知')
  })
})
