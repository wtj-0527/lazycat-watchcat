import type { Device, Metric } from './types'

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

export function metricValue(device: Device, name: string, digits = 1): string {
  const point = metric(device, name)
  return point ? `${formatNumber(point.value, digits)}${point.unit || ''}` : '—'
}

export function bytes(value: number): string {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${formatNumber(value / 1024)} KiB`
  if (value < 1024 ** 3) return `${formatNumber(value / 1024 ** 2)} MiB`
  return `${formatNumber(value / 1024 ** 3, 2)} GiB`
}

export function duration(seconds: number): string {
  const safe = Math.max(0, Number(seconds || 0))
  const days = Math.floor(safe / 86400)
  const hours = Math.floor((safe % 86400) / 3600)
  return days ? `${days} 天 ${hours} 小时` : `${hours} 小时`
}

export function signed(value?: number): string {
  const numeric = Number(value || 0)
  return numeric > 0 ? `+${numeric}` : String(numeric)
}

export function backupType(type: string): string {
  return ({ manual: '手动', 'pre-upgrade': '升级前', 'pre-restore': '恢复前' } as Record<string, string>)[type] || type
}
