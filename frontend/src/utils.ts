import type { Device, Inspection, Metric } from './types'

export function formatNumber(value: unknown, digits = 1): string {
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric.toFixed(digits) : '—'
}

export function ago(value?: string): string {
  if (!value || new Date(value).getUTCFullYear() < 2000) return '尚无数据'
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000)
  if (seconds < 60) return `${Math.round(seconds)} 秒前`
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时前`
  return `${Math.round(seconds / 86400)} 天前`
}

export function metric(device: Device, name: string): Metric | undefined {
  return device.latest?.[name]?.[0]
}

export function storageUsageMetric(device: Device): Metric | undefined {
  return ['filesystem.root.usage', 'btrfs.usage']
    .flatMap((name) => device.latest?.[name] || [])
    .sort((a, b) => b.value - a.value)[0]
}

export function storageRiskStatus(point: Metric): 'critical' | 'warning' | undefined {
  if (point.risk === 'critical' || point.risk === 'warning') return point.risk
  switch (point.name) {
    case 'filesystem.root.usage':
    case 'btrfs.usage':
      if (point.value >= 95) return 'critical'
      if (point.value >= 85) return 'warning'
      return undefined
    case 'disk.temperature':
      if (point.value >= 80) return 'critical'
      if (point.value >= 70) return 'warning'
      return undefined
    case 'disk.nvme.media_errors':
    case 'disk.nvme.critical_warning':
      return point.value > 0 ? 'critical' : undefined
    case 'disk.ata.reallocated_sectors':
      return point.value > 0 ? 'warning' : undefined
    default:
      return undefined
  }
}

export function storageRiskAdvice(point: Metric): string {
  if (point.name === 'disk.nvme.media_errors') return '检查 NVMe 健康与备份'
  if (point.name === 'disk.nvme.critical_warning') return '立即检查 NVMe 健康与备份'
  if (point.name === 'disk.ata.reallocated_sectors') return '检查磁盘坏扇区趋势与备份'
  if (point.name === 'disk.temperature') return '检查散热和负载'
  return '清理空间或扩容'
}

export function metricValue(device: Device, name: string, digits = 1): string {
  const point = metric(device, name)
  return point ? formatMetricValue(point.value, point.unit, digits) : '—'
}

export function metricValueAny(device: Device, names: string[], digits = 1): string {
  for (const name of names) {
    const value = metricValue(device, name, digits)
    if (value !== '—') return value
  }
  return '未知'
}

export function connectivityState(device: Device): 'online' | 'stale' | 'offline' {
  if (device.status === 'revoked' || !device.online) return 'offline'
  return device.stale ? 'stale' : 'online'
}

export function deviceState(device: Device): string {
  const health = device.health || 'unknown'
  if (health === 'critical') return 'critical'
  if (device.status === 'revoked') return 'revoked'
  const connectivity = connectivityState(device)
  if (connectivity === 'offline') return 'offline'
  if (health === 'warning') return 'warning'
  if (connectivity === 'stale') return 'stale'
  return health
}

export function inspectionState(inspection: Inspection): string {
  if (inspection.status !== 'completed') return inspection.status === 'failed' ? 'error' : inspection.status
  if (inspection.criticalCount > 0) return 'critical'
  if (inspection.warningCount > 0) return 'warning'
  if (inspection.deviceCount > 0 && inspection.healthyCount === inspection.deviceCount) return 'healthy'
  return 'unknown'
}

export function statusRank(status: string): number {
  return ({ critical: 0, offline: 1, revoked: 1, warning: 2, stale: 3, unknown: 4, healthy: 5 } as Record<string, number>)[status] ?? 4
}

export function percent(part: number, total: number): string {
  return total > 0 ? `${formatNumber(part / total * 100)}%` : '未知'
}

export function dateTime(value?: string): string {
  if (!value) return '未知'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString()
}

export function metricLabel(point?: Metric): string {
  if (!point) return '未知'
  const labels = point.labels || {}
  return labels.device || labels.mount || labels.sensor || labels.app || '系统资源'
}

export function bytes(value: number): string {
  const numeric = Number(value)
  if (!Number.isFinite(numeric) || numeric < 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let size = numeric
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  const digits = unit === 0 ? 0 : unit >= 3 ? 2 : 1
  return `${formatNumber(size, digits)} ${units[unit]}`
}

export function duration(seconds: number): string {
  const numeric = Number(seconds)
  if (!Number.isFinite(numeric) || numeric < 0) return '—'
  const safe = Math.floor(numeric)
  const days = Math.floor(safe / 86400)
  const hours = Math.floor((safe % 86400) / 3600)
  const minutes = Math.floor((safe % 3600) / 60)
  const remainingSeconds = safe % 60
  if (days) return `${days} 天${hours ? ` ${hours} 小时` : ''}`
  if (hours) return `${hours} 小时${minutes ? ` ${minutes} 分钟` : ''}`
  if (minutes) return `${minutes} 分钟${remainingSeconds ? ` ${remainingSeconds} 秒` : ''}`
  return `${remainingSeconds} 秒`
}

export function formatMetricValue(value: unknown, unit = '', digits = 1): string {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return '—'
  switch (unit.trim().toLowerCase()) {
    case 'bytes': return bytes(numeric)
    case 'seconds': return duration(numeric)
    case 'hours': return `${formatNumber(numeric, digits)} 小时`
    case 'celsius': return `${formatNumber(numeric, digits)} ℃`
    case 'count':
    case 'bitmask': return formatNumber(numeric, 0)
    case 'bool': return numeric >= 1 ? '是' : '否'
    case '%': return `${formatNumber(numeric, digits)}%`
    case 'rpm': return `${formatNumber(numeric, 0)} rpm`
    case '': return formatNumber(numeric, digits)
    default: return `${formatNumber(numeric, digits)} ${unit}`
  }
}

export function signed(value?: number): string {
  const numeric = Number(value || 0)
  return numeric > 0 ? `+${numeric}` : String(numeric)
}

export function backupType(type: string): string {
  return ({ manual: '手动', 'pre-upgrade': '升级前', 'pre-restore': '恢复前' } as Record<string, string>)[type] || type
}
