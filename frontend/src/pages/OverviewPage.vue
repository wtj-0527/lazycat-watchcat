<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Overview } from '@/types'
import { ago, deviceState, formatMetricValue, statusRank, storageRiskAdvice, storageRiskStatus, storageUsageMetric } from '@/utils'
import AlertRow from '@/components/AlertRow.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

const { data, loading, error, refresh } = usePolling(async () => {
  const result = await api<Overview>('/api/v1/overview')
  return { ...result, devices: result.devices || [], alerts: result.alerts || [] }
})
const orderedDevices = computed(() => [...(data.value?.devices || [])]
  .sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))))
const activeAlerts = computed(() => data.value?.alerts || [])
const storageRows = computed(() => orderedDevices.value.map((device) => ({ device, point: storageUsageMetric(device) }))
  .filter((row) => row.point)
  .sort((a, b) => (b.point?.value || 0) - (a.point?.value || 0)))
function capabilitySummary(device: Device): string {
  const latest = Object.keys(device.latest || {})
  const available = ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.']
    .filter((prefix) => latest.some((name) => name.startsWith(prefix))).length
  return available ? `可用 ${available}` : '未知'
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="overview-layout-v2">
      <section class="card device-health-card">
        <div class="section-title">
          <div><h2>设备群健康</h2></div>
          <div class="status-summary"><StatusPill status="healthy" /><StatusPill status="warning" /><StatusPill status="critical" /></div>
        </div>
        <div v-if="orderedDevices.length" class="device-health-list">
          <div v-for="device in orderedDevices.slice(0, 5)" :key="device.id" class="device-health-row">
            <i :class="deviceState(device)" />
            <b>{{ device.name }}</b>
            <StatusPill :status="deviceState(device)" />
            <span>{{ device.online ? (device.stale ? '陈旧' : '在线') : '离线' }}</span>
            <span>{{ ago(device.lastSeenAt) }}</span>
            <a href="#devices">查看 →</a>
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
      <div class="table-scroll">
        <table class="fleet-table overview-matrix">
          <thead><tr><th>设备 / 卷</th><th>当前使用率</th><th>采集能力</th><th>预计写满</th><th>风险</th><th>建议</th></tr></thead>
          <tbody>
            <tr v-for="row in storageRows.slice(0, 5)" :key="row.device.id">
              <td class="device"><b>{{ row.device.name }}</b><small>{{ row.point?.labels?.mount || row.point?.labels?.device || '主存储' }}</small></td>
              <td><b>{{ row.point ? formatMetricValue(row.point.value, row.point.unit) : '未知' }}</b></td>
              <td>{{ capabilitySummary(row.device) }}</td>
              <td>未知</td>
              <td><StatusPill :status="row.point && storageRiskStatus(row.point) || 'unknown'" /></td>
              <td>{{ row.point ? storageRiskAdvice(row.point) : '等待采集' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!storageRows.length" class="inline-empty">尚无可用于容量判断的真实存储指标。</div>
    </section>
  </PageState>
</template>
