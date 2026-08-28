<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Alert, Device, Metric, Overview } from '@/types'
import { ago, bytes, deviceState, formatMetricValue, statusRank, storageRiskAdvice, storageRiskStatus, storageUsageMetrics } from '@/utils'
import BarChart from '@/components/BarChart.vue'
import DonutChart from '@/components/DonutChart.vue'
import PageState from '@/components/PageState.vue'
import RealtimeMetricCard from '@/components/RealtimeMetricCard.vue'
import StatusPill from '@/components/StatusPill.vue'

const realtime = ref(false)
const pollingInterval = computed(() => realtime.value ? 5_000 : 30_000)
let realtimeTimeout: number | undefined
const { data, loading, error, refresh } = usePolling(async () => {
  const result = await api<Overview>('/api/v1/overview')
  return { ...result, devices: result.devices || [], alerts: result.alerts || [] }
}, pollingInterval)
function toggleRealtime() {
  realtime.value = !realtime.value
  window.clearTimeout(realtimeTimeout)
  if (realtime.value) {
    realtimeTimeout = window.setTimeout(() => {
      realtime.value = false
    }, 10 * 60_000)
  }
}
onBeforeUnmount(() => window.clearTimeout(realtimeTimeout))
const orderedDevices = computed(() => [...(data.value?.devices || [])]
  .sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))))
const storageRows = computed(() => orderedDevices.value
  .flatMap((device) => storageUsageMetrics(device).map((point) => ({ device, point })))
  .sort((a, b) => b.point.value - a.point.value))
const healthDistribution = computed(() => [
  { label: '健康', value: orderedDevices.value.filter((device) => deviceState(device) === 'healthy').length, color: '#118847' },
  { label: '警告', value: orderedDevices.value.filter((device) => deviceState(device) === 'warning').length, color: '#c05600' },
  { label: '严重', value: orderedDevices.value.filter((device) => deviceState(device) === 'critical').length, color: '#c51d23' },
  { label: '离线', value: orderedDevices.value.filter((device) => deviceState(device) === 'offline').length, color: '#64748b' },
])
const capacityBars = computed(() => storageRows.value.map((row) => ({
  label: `${row.device.name} / ${row.point?.labels?.mount || row.point?.labels?.device || '主存储'}`,
  value: Number((row.point?.value || 0).toFixed(1)),
  color: row.point && storageRiskStatus(row.point) === 'critical' ? '#c51d23' : row.point && storageRiskStatus(row.point) === 'warning' ? '#c05600' : '#2563eb',
  hint: row.point ? storageRiskAdvice(row.point) : '等待采集',
})))
interface RealtimeMetric {
  label: string
  value: string
  detail: string
  parts?: Array<{ label: string; value: string }>
  percent?: number
  status?: 'warning' | 'critical'
}
interface DeviceRiskEvidence {
  key: string
  severity: 'warning' | 'critical'
  message: string
  resource: string
}
function metricPoints(device: Device, names: string[]): Metric[] {
  for (const name of names) {
    const points = device.latest?.[name] || []
    if (points.length) return points
  }
  return []
}
function pointDetail(points: Metric[]): string {
  if (!points.length) return '尚未采集到该指标'
  const labels = points.map((point) => point.labels?.sensor || point.labels?.interface || point.labels?.device || point.labels?.mount).filter(Boolean)
  return `${points[0].name}${labels.length ? ` · ${[...new Set(labels)].join('、')}` : ''} · ${ago(points[0].collectedAt)}`
}
function singleMetric(device: Device, label: string, names: string[], thresholds?: [number, number]): RealtimeMetric {
  const points = metricPoints(device, names)
  const point = points[0]
  if (!point) return { label, value: '未知', detail: '尚未采集到该指标' }
  const status = thresholds ? point.value >= thresholds[1] ? 'critical' : point.value >= thresholds[0] ? 'warning' : undefined : undefined
  return {
    label,
    value: formatMetricValue(point.value, point.unit),
    detail: pointDetail(points),
    percent: point.unit === '%' ? Math.max(0, Math.min(100, point.value)) : undefined,
    status,
  }
}
function temperatureSource(point: Metric): string {
  if (point.name === 'disk.temperature') {
    return point.labels?.model || (point.labels?.device ? `/dev/${String(point.labels.device).replace('/dev/', '')}` : '磁盘 SMART')
  }
  const sensor = String(point.labels?.sensor || '').toLowerCase()
  if (sensor.startsWith('coretemp_package') || sensor === 'package') return 'CPU 封装'
  if (sensor.startsWith('coretemp_core')) return 'CPU 核心'
  if (sensor.startsWith('nvme_composite')) return 'NVMe 综合'
  if (sensor.startsWith('nvme_sensor_')) return 'NVMe 内部传感器'
  if (sensor.startsWith('spd5118')) return '内存模组'
  if (sensor.startsWith('iwlwifi')) return '无线网卡'
  if (sensor.startsWith('acpitz')) return '主板 ACPI'
  return point.labels?.sensor || '硬件传感器'
}
function temperatureThresholds(point: Metric): [number, number] {
  if (point.name === 'disk.temperature') return [70, 80]
  const sensor = String(point.labels?.sensor || '').toLowerCase()
  if (sensor.startsWith('coretemp_package') || sensor.startsWith('coretemp_core') || sensor === 'package') return [90, 100]
  if (sensor.startsWith('nvme_composite')) return [85, 90]
  if (sensor.startsWith('spd5118')) return [55, 85]
  return [80, 95]
}
function temperatureMetric(device: Device): RealtimeMetric {
  const system = device.latest?.['system.temperature'] || []
  const disk = device.latest?.['disk.temperature'] || []
  const hasPackage = system.some((point) => {
    const sensor = String(point.labels?.sensor || '').toLowerCase()
    return sensor.startsWith('coretemp_package') || sensor === 'package'
  })
  const hasNVMeComposite = system.some((point) => String(point.labels?.sensor || '').toLowerCase().startsWith('nvme_composite'))
  const candidates = [...system, ...disk].filter((point) => {
    const sensor = String(point.labels?.sensor || '').toLowerCase()
    if (hasPackage && sensor.startsWith('coretemp_core')) return false
    // NVMe sensor_1/2 are vendor-specific hotspot or controller readings.
    // Prefer the standards-based composite temperature when it is available.
    if (hasNVMeComposite && sensor.startsWith('nvme_sensor_')) return false
    return true
  })
  if (!candidates.length) return { label: '最高温度', value: '未知', detail: '尚未采集到该指标' }
  const point = [...candidates].sort((a, b) => b.value - a.value)[0]
  const source = temperatureSource(point)
  const thresholds = temperatureThresholds(point)
  const status = point.value >= thresholds[1] ? 'critical' : point.value >= thresholds[0] ? 'warning' : undefined
  const raw = point.labels?.sensor || point.labels?.device || point.name
  return {
    label: `${source}温度`,
    value: formatMetricValue(point.value, point.unit),
    detail: `${raw} · ${point.labels?.model ? `${point.labels.model} · ` : ''}${ago(point.collectedAt)}`,
    status,
  }
}
function total(device: Device, primary: string, fallback: string): { value: number; points: Metric[] } {
  const direct = device.latest?.[primary] || []
  const points = direct.length ? direct : (device.latest?.[fallback] || [])
  return { value: points.reduce((sum, point) => sum + point.value, 0), points }
}
function pairedCounter(device: Device, label: string, left: [string, string, string], right: [string, string, string]): RealtimeMetric {
  const a = total(device, left[0], left[1])
  const b = total(device, right[0], right[1])
  const points = [...a.points, ...b.points]
  return {
    label,
    value: points.length ? `${left[2]} ${bytes(a.value)} · ${right[2]} ${bytes(b.value)}` : '未知',
    parts: points.length ? [{ label: left[2], value: bytes(a.value) }, { label: right[2], value: bytes(b.value) }] : undefined,
    detail: pointDetail(points),
  }
}
function realtimeMetrics(device: Device): RealtimeMetric[] {
  const storage = storageUsageMetrics(device).sort((a, b) => b.value - a.value)[0]
  return [
    singleMetric(device, 'CPU 使用率', ['system.cpu.usage'], [85, 95]),
    singleMetric(device, '内存使用率', ['system.memory.usage'], [85, 95]),
    singleMetric(device, '1 分钟负载', ['system.load.1m']),
    temperatureMetric(device),
    storage
      ? { label: '最高存储使用率', value: formatMetricValue(storage.value, storage.unit), detail: pointDetail([storage]), percent: storage.value, status: storage.value >= 95 ? 'critical' : storage.value >= 85 ? 'warning' : undefined }
      : { label: '最高存储使用率', value: '未知', detail: '尚未采集到该指标' },
    singleMetric(device, '运行时间', ['system.uptime']),
    pairedCounter(device, '网络累计流量', ['network.receive.bytes_total', 'network.interface.receive.bytes_total', '收'], ['network.transmit.bytes_total', 'network.interface.transmit.bytes_total', '发']),
    pairedCounter(device, '磁盘累计 I/O', ['disk.io.read.bytes_total', 'disk.io.read.bytes_total', '读'], ['disk.io.write.bytes_total', 'disk.io.write.bytes_total', '写']),
  ]
}
function metricRiskMessage(point: Metric): string {
  const labels: Record<string, string> = {
    'system.cpu.usage': 'CPU 使用率',
    'system.memory.usage': '内存使用率',
    'system.temperature': '硬件温度',
    'filesystem.root.usage': '文件系统使用率',
    'filesystem.volume.usage': '存储卷使用率',
    'btrfs.usage': 'Btrfs 使用率',
    'container.memory.usage_percent': '容器内存使用率',
    'disk.temperature': '磁盘温度',
    'disk.nvme.media_errors': 'NVMe 介质错误',
    'disk.nvme.critical_warning': 'NVMe 严重警告',
    'disk.ata.reallocated_sectors': '重映射扇区',
    'disk.ata.pending_sectors': '待处理扇区',
    'disk.ata.offline_uncorrectable': '离线不可校正扇区',
    'disk.ata.reported_uncorrectable': '已报告不可校正错误',
    'lpk.application.healthy': '应用状态异常',
  }
  if (point.name === 'disk.nvme.critical_warning') return `${labels[point.name]} 0x${Math.round(point.value).toString(16).toUpperCase()}`
  if (point.name === 'lpk.application.healthy') return `${point.labels?.app || '应用'} 状态异常`
  return `${labels[point.name] || point.name} ${formatMetricValue(point.value, point.unit)}`
}
function metricResource(point: Metric): string {
  return point.labels?.device || point.labels?.mount || point.labels?.app || point.labels?.sensor || point.name
}
function alertEvidence(alert: Alert): DeviceRiskEvidence {
  return {
    key: alert.fingerprint,
    severity: alert.severity === 'critical' ? 'critical' : 'warning',
    message: alert.message || alert.resource,
    resource: alert.resource,
  }
}
function deviceRiskEvidence(device: Device): DeviceRiskEvidence[] {
  const activeAlerts = (data.value?.alerts || [])
    .filter((alert) => alert.status !== 'resolved'
      && (alert.deviceId === device.id || (!alert.deviceId && alert.deviceName === device.name))
      && (alert.severity === 'critical' || alert.severity === 'warning'))
    .map(alertEvidence)
  const metricEvidence = Object.values(device.latest || {}).flatMap((points) => points
    .filter((point) => point.risk === 'critical' || point.risk === 'warning')
    .map((point) => ({
      key: `${point.name}:${metricResource(point)}`,
      severity: point.risk as 'warning' | 'critical',
      message: metricRiskMessage(point),
      resource: metricResource(point),
    })))
  const unique = new Map<string, DeviceRiskEvidence>()
  for (const item of [...activeAlerts, ...metricEvidence]) {
    const key = `${item.severity}:${item.message}:${item.resource}`
    if (!unique.has(key)) unique.set(key, item)
  }
  return [...unique.values()].sort((a, b) => statusRank(a.severity) - statusRank(b.severity))
}
function capabilitySummary(device: Device): string {
  const latest = Object.keys(device.latest || {})
  const available = ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.']
    .filter((prefix) => latest.some((name) => name.startsWith(prefix))).length
  return available ? `${available}/5` : '未知'
}
function capabilityDetail(device: Device): string {
  const latest = Object.keys(device.latest || {})
  const capabilities = [
    ['系统', 'system.'], ['容器', 'container.'], ['文件系统', 'filesystem.'],
    ['磁盘 SMART', 'disk.'], ['Btrfs', 'btrfs.'],
  ]
  return capabilities.map(([label, prefix]) => `${label}：${latest.some((name) => name.startsWith(prefix)) ? '可用' : '缺失'}`).join('；')
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="overview-summary-grid">
      <section class="card overview-health-distribution-card">
        <div class="section-title"><div><h2>设备健康构成</h2></div><span class="overview-card-meta">{{ orderedDevices.length }} 台</span></div>
        <DonutChart :items="healthDistribution" center-label="设备" />
      </section>

      <section class="card overview-storage-card">
        <div class="section-title"><div><h2>存储卷使用率</h2></div><span class="overview-card-meta">{{ storageRows.length }} 个卷</span></div>
        <BarChart :items="capacityBars" unit="%" />
      </section>

    </div>

    <section class="fleet-realtime-section">
      <div class="realtime-mode-bar">
        <button
          class="realtime-mode-button"
          :class="{ active: realtime }"
          type="button"
          :title="realtime ? '关闭后恢复每 30 秒刷新' : '每 5 秒读取一次最新数据，10 分钟后自动关闭'"
          :aria-pressed="realtime"
          @click="toggleRealtime"
        >
          <i />{{ realtime ? '实时 · 5 秒' : '开启实时' }}
        </button>
      </div>
      <div v-if="orderedDevices.length" class="fleet-realtime-grid">
        <article v-for="device in orderedDevices" :key="device.id" class="fleet-device-metrics">
          <header>
            <div><i :class="deviceState(device)" /><span><b>{{ device.name }}</b><small>{{ device.hostname || device.id }} · {{ ago(device.lastSeenAt) }}</small></span></div>
            <StatusPill :status="deviceState(device)" />
          </header>
          <div v-if="deviceRiskEvidence(device).length" class="device-health-evidence" :class="deviceState(device)">
            <span class="device-health-evidence-label">{{ deviceState(device)==='critical'?'严重原因':'警告原因' }}</span>
            <div>
              <span v-for="item in deviceRiskEvidence(device).slice(0,3)" :key="item.key" class="device-health-evidence-item" :class="item.severity">
                <b>{{ item.message }}</b><small>{{ item.resource }}</small>
              </span>
            </div>
            <a href="#alerts">{{deviceRiskEvidence(device).length>3?`查看全部 ${deviceRiskEvidence(device).length} 项`:'查看告警'}}</a>
          </div>
          <div class="realtime-metric-grid">
            <RealtimeMetricCard
              v-for="(item, index) in realtimeMetrics(device)"
              :key="item.label"
              :label="item.label"
              :value="item.value"
              :parts="item.parts"
              :detail="item.detail"
              :percent="item.percent"
              :status="item.status"
              :tooltip-placement="index < 4 ? 'below' : 'above'"
            />
          </div>
        </article>
      </div>
      <div v-else class="inline-empty">设备接入并完成首次采集后，将在此显示 CPU、内存、温度、存储和 I/O 指标。</div>
    </section>

    <section class="card capacity-risk-card">
      <div class="section-title"><div><h2>容量风险与预计写满时间</h2></div></div>
      <div class="capacity-evidence-list" role="table" aria-label="容量风险与预计写满时间">
        <div class="capacity-evidence-head" role="row">
          <span role="columnheader">设备 / 卷</span><span role="columnheader">当前使用率</span>
          <span role="columnheader">采集能力</span><span role="columnheader">预计写满</span>
          <span role="columnheader">风险</span><span role="columnheader">建议</span>
        </div>
        <div v-for="row in storageRows" :key="`${row.device.id}:${row.point.labels?.mount || row.point.labels?.device || row.point.name}`" class="capacity-evidence-row" role="row">
          <div role="cell" data-label="设备 / 卷"><b>{{ row.device.name }}</b><small>{{ row.point?.labels?.mount || row.point?.labels?.device || '主存储' }}</small></div>
          <strong role="cell" data-label="当前使用率">{{ row.point ? formatMetricValue(row.point.value, row.point.unit) : '未知' }}</strong>
          <span class="capability-summary-hover" role="cell" data-label="采集能力" tabindex="0">采集能力 {{ capabilitySummary(row.device) }}<span class="capacity-hover-tooltip" role="tooltip">{{ capabilityDetail(row.device) }}</span></span>
          <span role="cell" data-label="预计写满">历史不足</span>
          <span role="cell" data-label="风险"><StatusPill :status="row.point && storageRiskStatus(row.point) || 'unknown'" /></span>
          <p role="cell" data-label="建议">{{ row.point ? storageRiskAdvice(row.point) : '等待采集' }}</p>
        </div>
      </div>
      <div v-if="!storageRows.length" class="inline-empty">尚无可用于容量判断的真实存储指标。</div>
    </section>
  </PageState>
</template>
