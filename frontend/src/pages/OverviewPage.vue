<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Overview } from '@/types'
import { ago, deviceState, metric, metricValueAny, percent, statusRank } from '@/utils'
import AlertRow from '@/components/AlertRow.vue'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

const { data, loading, error, refresh } = usePolling(() => api<Overview>('/api/v1/overview'))

const orderedDevices = computed(() =>
  [...(data.value?.devices || [])].sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))),
)
const storageRisk = computed(() => (data.value?.devices || []).filter((device) => {
  const point = metric(device, 'filesystem.root.usage') || metric(device, 'btrfs.usage')
  return point != null && point.value >= 85
}).length)
const incident = computed(() => {
  const offline = data.value?.stats.offline ?? 0
  const critical = data.value?.stats.critical ?? 0
  if (!offline && !critical) return undefined
  return {
    title: critical ? `${critical} 台设备处于 Critical` : `${offline} 台设备离线`,
    text: `当前 Fleet 存在 ${critical} 台严重异常、${offline} 台离线设备，请优先检查风险列表。`,
  }
})
const stat = (name: string) => data.value?.stats[name] ?? 'Unknown'

function network(device: Device): string {
  return metricValueAny(device, ['container.network.receive.bytes_total', 'system.network.receive.bytes_total'])
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div v-if="incident" class="incident-banner">
      <div class="incident-mark">!</div>
      <div><b>{{ incident.title }}</b><p>{{ incident.text }}</p></div>
      <StatusPill status="critical" />
    </div>

    <div class="stats">
      <StatCard label="设备" :value="stat('devices')" hint="已注册设备" />
      <StatCard label="在线" :value="stat('online')" :hint="typeof stat('devices') === 'number' ? percent(Number(stat('online')), Number(stat('devices'))) : 'Unknown'" tone="green" />
      <StatCard label="离线" :value="stat('offline')" hint="需要检查" :tone="Number(stat('offline')) ? 'amber' : 'green'" />
      <StatCard label="Critical" :value="stat('critical')" hint="需要立即处理" :tone="Number(stat('critical')) ? 'red' : 'green'" />
      <StatCard label="Warning" :value="stat('warning')" hint="需要关注" :tone="Number(stat('warning')) ? 'amber' : 'green'" />
      <StatCard label="存储风险" :value="storageRisk" hint="使用率 ≥ 85%" :tone="storageRisk ? 'amber' : 'green'" />
    </div>

    <div class="overview-layout">
      <section class="card matrix-card">
        <div class="section-title">
          <div>
            <h2>设备健康矩阵</h2>
            <span class="muted">按严重程度排序 · 数据每 30 秒更新</span>
          </div>
          <span class="filter-chip">所有设备</span>
        </div>
        <div v-if="orderedDevices.length" class="table-scroll">
          <table class="fleet-table overview-matrix">
            <thead><tr><th>设备</th><th>CPU</th><th>内存</th><th>存储</th><th>磁盘</th><th>应用</th><th>网络</th><th>状态</th></tr></thead>
            <tbody>
              <tr v-for="device in orderedDevices" :key="device.id">
                <td class="device"><b>{{ device.name }}</b><small>未分组 · {{ ago(device.lastSeenAt) }}</small></td>
                <td>{{ metricValueAny(device, ['system.cpu.usage', 'system.load.1m']) }}</td>
                <td>{{ metricValueAny(device, ['system.memory.usage']) }}</td>
                <td>{{ metricValueAny(device, ['filesystem.root.usage', 'btrfs.usage']) }}</td>
                <td>{{ metricValueAny(device, ['disk.temperature', 'disk.nvme.media_errors'], 0) }}</td>
                <td><span class="contract-gap">Contract gap</span></td>
                <td>{{ network(device) }}</td>
                <td><StatusPill :status="deviceState(device)" /></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="inline-empty">尚未接入设备。设备接入后将在此显示实时健康矩阵。</div>
      </section>

      <aside class="overview-side">
        <section class="card">
          <div class="section-title compact">
            <div><h2>当前风险</h2><span class="muted">{{ data?.alerts.length ?? 0 }} 条活动风险</span></div>
          </div>
          <div v-if="data?.alerts.length" class="compact-alerts">
            <AlertRow v-for="alert in data.alerts.slice(0, 5)" :key="alert.fingerprint" :alert="alert" />
          </div>
          <div v-else class="healthy-empty">
            <span>✓</span><b>当前没有活动风险</b><small>基于最近一次有效采集证据</small>
          </div>
        </section>
        <section class="card freshness-card">
          <div class="section-title compact"><div><h2>数据新鲜度</h2><span class="muted">Collector Snapshot</span></div></div>
          <div class="freshness-line"><i /><div><b>最近一次采集</b><span>{{ ago(data?.updatedAt) }}</span></div></div>
          <p>页面每 30 秒自动刷新。离线和过期设备不会继续显示为 Healthy。</p>
        </section>
      </aside>
    </div>
  </PageState>
</template>
