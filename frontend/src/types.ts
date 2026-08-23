export type Health = 'healthy' | 'warning' | 'critical' | 'unknown'
export type Connectivity = 'online' | 'stale' | 'offline'
export type CapabilityStatus = 'available' | 'restricted' | 'unsupported' | 'error' | 'unknown'

export interface Metric {
  deviceId?: string
  deviceName?: string
  name: string
  value: number
  unit: string
  labels: Record<string, string> | null
  collectedAt: string
}

export interface Device {
  id: string
  name: string
  hostname: string
  osVersion: string
  collectorVersion: string
  status: string
  lastSeenAt: string
  online: boolean
  stale: boolean
  health: Health
  latest: Record<string, Metric[]>
}

export interface Alert {
  fingerprint: string
  deviceId?: string
  deviceName: string
  severity: Health
  resource: string
  message: string
  value: number
  unit: string
  status: string
  lastSeenAt?: string
  observedAt?: string
  collectedAt?: string
}

export interface Overview {
  stats: Record<string, number>
  devices: Device[]
  alerts: Alert[]
  updatedAt: string
}

export interface Inspection {
  id: string
  triggerType: string
  startedAt: string
  status: string
  deviceCount: number
  healthyCount: number
  warningCount: number
  criticalCount: number
  evidenceSha256: string
  completedAt?: string
  error?: string
  report?: {
    schemaVersion?: number
    generatedAt?: string
    source?: string
    latestMetricAt?: string
    checks?: Record<string, number>
    devices?: Device[]
    alerts?: Alert[]
  }
  changeSummary?: {
    warningDelta?: number
    criticalDelta?: number
    newAlerts?: unknown[]
    resolvedAlerts?: unknown[]
  }
}

export interface Backup {
  name: string
  type: string
  appVersion: string
  createdAt: string
  size: number
  sha256: string
  verified: boolean
}

export interface Stability {
  startedAt: string
  targetEndAt: string
  lastSampleAt: string
  sampleCount: number
  failureCount: number
  consecutiveFailures: number
  databaseIntegrityOk: boolean
  databaseLatencyMs: number
  latestMetricAt?: string
  metricFreshnessSeconds?: number
  pendingNotifications: number
  qualified: boolean
  remainingSeconds: number
}

export interface Capability {
  capability: string
  status: CapabilityStatus
  detail: string
  checkedAt: string
}

export interface ApplicationDevice {
  deviceId: string
  deviceName: string
  deployId: string
  healthy: boolean
  status: string
  installStatus: string
  version: string
  domain: string
  builtin: boolean
  collectedAt: string
}

export interface ApplicationItem {
  id: string
  title: string
  instances: number
  healthy: number
  unhealthy: number
  paused: number
  versions: Record<string, number>
  statusCounts: Record<string, number>
  devices: ApplicationDevice[]
  resources: {
    containers: number
    cpuPercent: number
    memoryUsage: number
    memoryLimit: number
    networkReceive: number
    networkTransmit: number
    blockRead: number
    blockWrite: number
    updatedAt?: string
  }
}
