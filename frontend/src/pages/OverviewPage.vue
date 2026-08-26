<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Overview } from '@/types'
import { ago, deviceState, formatMetricValue, statusRank, storageRiskAdvice, storageRiskStatus, storageUsageMetrics } from '@/utils'
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
