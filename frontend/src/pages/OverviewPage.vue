<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Metric, Overview } from '@/types'
import { ago, bytes, deviceState, formatMetricValue, statusRank, storageRiskAdvice, storageRiskStatus, storageUsageMetrics } from '@/utils'
import AlertRow from '@/components/AlertRow.vue'
import BarChart from '@/components/BarChart.vue'
import DonutChart from '@/components/DonutChart.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

const { data, loading, error, refresh } = usePolling(async () => {
  const result = await api<Overview>('/api/v1/overview')
  return { ...result, devices: result.devices || [], alerts: result.alerts || [] }
})
const orderedDevices = computed(() => [...(data.value?.devices || [])]
  .sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))))
const activeAlerts = computed(() => data.value?.alerts || [])
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
  percent?: number
  status?: 'warning' | 'critical'
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
function maxMetric(device: Device, label: string, names: string[], thresholds?: [number, number]): RealtimeMetric {
  const points = metricPoints(device, names)
  if (!points.length) return { label, value: '未知', detail: '尚未采集到该指标' }
  const point = [...points].sort((a, b) => b.value - a.value)[0]
  const status = thresholds ? point.value >= thresholds[1] ? 'critical' : point.value >= thresholds[0] ? 'warning' : undefined : undefined
  return { label, value: formatMetricValue(point.value, point.unit), detail: pointDetail([point]), status }
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
    detail: pointDetail(points),
  }
}
function realtimeMetrics(device: Device): RealtimeMetric[] {
  const storage = storageUsageMetrics(device).sort((a, b) => b.value - a.value)[0]
  return [
    singleMetric(device, 'CPU 使用率', ['system.cpu.usage'], [85, 95]),
    singleMetric(device, '内存使用率', ['system.memory.usage'], [85, 95]),
    singleMetric(device, '1 分钟负载', ['system.load.1m']),
    maxMetric(device, '最高温度', ['system.temperature', 'disk.temperature'], [70, 80]),
    storage
      ? { label: '最高存储使用率', value: formatMetricValue(storage.value, storage.unit), detail: pointDetail([storage]), percent: storage.value, status: storage.value >= 95 ? 'critical' : storage.value >= 85 ? 'warning' : undefined }
      : { label: '最高存储使用率', value: '未知', detail: '尚未采集到该指标' },
    singleMetric(device, '运行时间', ['system.uptime']),
    pairedCounter(device, '网络累计流量', ['network.receive.bytes_total', 'network.interface.receive.bytes_total', '收'], ['network.transmit.bytes_total', 'network.interface.transmit.bytes_total', '发']),
    pairedCounter(device, '磁盘累计 I/O', ['disk.io.read.bytes_total', 'disk.io.read.bytes_total', '读'], ['disk.io.write.bytes_total', 'disk.io.write.bytes_total', '写']),
  ]
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

      <section class="card device-health-card">
        <div class="section-title">
          <div><h2>设备群健康</h2></div>
          <div class="status-summary"><StatusPill status="healthy" /><StatusPill status="warning" /><StatusPill status="critical" /></div>
        </div>
        <div v-if="orderedDevices.length" class="device-health-list" role="table" aria-label="设备群健康">
          <div class="device-health-head" role="row">
            <span aria-hidden="true" /><span role="columnheader">设备</span><span role="columnheader">健康</span>
            <span role="columnheader">连接</span><span role="columnheader">最新数据</span><span role="columnheader">操作</span>
          </div>
          <div v-for="device in orderedDevices.slice(0, 5)" :key="device.id" class="device-health-row" role="row">
            <i :class="deviceState(device)" aria-hidden="true" />
            <b role="cell" data-label="设备">{{ device.name }}</b>
            <span role="cell" data-label="健康"><StatusPill :status="deviceState(device)" /></span>
            <span role="cell" data-label="连接">{{ device.online ? (device.stale ? '陈旧' : '在线') : '离线' }}</span>
            <span role="cell" data-label="最新数据">{{ ago(device.lastSeenAt) }}</span>
            <a role="cell" data-label="操作" href="#devices">查看 →</a>
          </div>
        </div>
        <div v-else class="inline-empty">尚未接入设备。设备接入后将在此显示实时健康状态。</div>
      </section>

      <aside class="card pending-events">
        <div class="section-title compact"><div><h2>待处理事件</h2></div><span class="pill critical">{{ activeAlerts.length }} 个</span></div>
        <div v-if="activeAlerts.length" class="compact-alerts">
          <AlertRow v-for="alert in activeAlerts.slice(0, 3)" :key="alert.fingerprint" :alert="alert" />
          <a class="section-link" href="#alerts">进入告警工作台 →</a>
        </div>
        <div v-else class="healthy-empty"><span>✓</span><b>当前没有活动风险</b></div>
      </aside>
    </div>

    <section class="card fleet-realtime-card">
      <div class="section-title">
        <div><h2>设备实时指标</h2></div>
        <span class="overview-card-meta">{{ orderedDevices.length }} 台设备</span>
      </div>
      <div v-if="orderedDevices.length" class="fleet-realtime-grid">
        <article v-for="device in orderedDevices" :key="device.id" class="fleet-device-metrics">
          <header>
            <div><i :class="deviceState(device)" /><span><b>{{ device.name }}</b><small>{{ device.hostname || device.id }} · {{ ago(device.lastSeenAt) }}</small></span></div>
            <StatusPill :status="deviceState(device)" />
          </header>
          <div class="realtime-metric-grid">
            <div v-for="item in realtimeMetrics(device)" :key="item.label" class="realtime-metric" :class="item.status" :title="item.detail">
              <span>{{ item.label }}</span>
              <strong>{{ item.value }}</strong>
              <i v-if="item.percent !== undefined"><em :style="{width:`${item.percent}%`}" /></i>
              <small>{{ item.detail }}</small>
            </div>
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
          <span role="cell" data-label="采集能力" :title="capabilityDetail(row.device)">采集能力 {{ capabilitySummary(row.device) }}</span>
          <span role="cell" data-label="预计写满">历史不足</span>
          <span role="cell" data-label="风险"><StatusPill :status="row.point && storageRiskStatus(row.point) || 'unknown'" /></span>
          <p role="cell" data-label="建议">{{ row.point ? storageRiskAdvice(row.point) : '等待采集' }}</p>
        </div>
      </div>
      <div v-if="!storageRows.length" class="inline-empty">尚无可用于容量判断的真实存储指标。</div>
    </section>
  </PageState>
</template>
