<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { ApplicationItem } from '@/types'
import { ago, bytes, formatNumber, percent } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: ApplicationItem[]; source: string; stale: boolean; updatedAt?: string }
const query = ref('')
const statusFilter = ref('all')
const { data, loading, error, refresh } = usePolling(() => api<Payload>('/api/v1/applications'))
const instances = computed(() => data.value?.items.reduce((sum, item) => sum + item.instances, 0) ?? 0)
const healthy = computed(() => data.value?.items.reduce((sum, item) => sum + item.healthy, 0) ?? 0)
const paused = computed(() => data.value?.items.reduce((sum, item) => sum + item.paused, 0) ?? 0)
const errors = computed(() => data.value?.items.reduce((sum, item) => sum + item.unhealthy, 0) ?? 0)
const versionDrift = computed(() => data.value?.items.filter((item) => Object.keys(item.versions).length > 1).length ?? 0)
const appStatus = (item: ApplicationItem) => item.unhealthy > 0 ? 'critical' : item.paused > 0 ? 'warning' : item.healthy > 0 ? 'healthy' : 'unknown'
const filtered = computed(() => (data.value?.items || []).filter((item) => {
  const matchesQuery = `${item.title} ${item.id}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const status = appStatus(item)
  const matchesStatus = statusFilter.value === 'all'
    || (statusFilter.value === 'healthy' && status === 'healthy')
    || (statusFilter.value === 'degraded' && status === 'warning')
    || (statusFilter.value === 'critical' && status === 'critical')
  return matchesQuery && matchesStatus
}))
const statusLabel = (status: string) => ({ running: '运行中', paused: '已暂停', starting: '启动中', stopping: '停止中', error: '异常' } as Record<string, string>)[status] || status || 'Unknown'
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="LazyCat Package Manager 尚未返回当前用户的应用状态。" @retry="refresh">
    <div class="page-intro">
      <div><h2>应用健康</h2><p>从应用视角定位版本漂移、容器异常与跨设备故障。</p></div>
      <span v-if="data?.stale" class="stale-banner">Stale · 当前显示最近一次成功快照</span>
    </div>
    <div class="stats five">
      <StatCard label="LPK 应用" :value="data?.items.length ?? 0" hint="全 Fleet" />
      <StatCard label="运行正常" :value="healthy" :hint="percent(healthy, instances)" tone="green" />
      <StatCard label="Degraded" :value="paused" hint="暂停或未完全运行" :tone="paused ? 'amber' : 'green'" />
      <StatCard label="Critical" :value="errors" hint="实例异常" :tone="errors ? 'red' : 'green'" />
      <StatCard label="版本漂移" :value="versionDrift" hint="跨设备版本不一致" :tone="versionDrift ? 'amber' : 'green'" />
    </div>
    <div class="filter-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索应用名称或 App ID"></label>
      <select v-model="statusFilter"><option value="all">全部状态</option><option value="healthy">运行正常</option><option value="degraded">Degraded</option><option value="critical">Critical</option></select>
      <span class="filter-note">来源：LazyCat Package Manager · 更新 {{ ago(data?.updatedAt) }}</span>
    </div>
    <section class="card">
      <div class="section-title"><div><h2>应用矩阵</h2><span class="muted">每一行展示真实应用部署、实例、版本和容器资源</span></div></div>
      <div class="table-scroll">
        <table class="fleet-table app-matrix">
          <thead><tr><th>应用部署</th><th>结果</th><th>设备实例</th><th>版本</th><th>运行状态</th><th>容器资源</th><th>网络 / 块 IO</th></tr></thead>
          <tbody>
            <tr v-for="item in filtered" :key="item.id">
              <td class="device"><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small></td>
              <td><StatusPill :status="appStatus(item)" /></td>
              <td>
                <div v-if="item.devices?.length" class="instance-stack">
                  <span v-for="device in item.devices" :key="device.deployId"><i :class="device.status" />{{ device.deviceName || 'Unknown device' }} · {{ statusLabel(device.status) }}</span>
                </div>
                <span v-else class="contract-gap">Unknown</span>
              </td>
              <td>{{ Object.entries(item.versions).map(([version, count]) => `${version || 'Unknown'} × ${count}`).join(' · ') || 'Unknown' }}</td>
              <td><span v-for="([status, count]) in Object.entries(item.statusCounts)" :key="status" class="runtime-chip" :class="status">{{ statusLabel(status) }} × {{ count }}</span></td>
              <td>
                <template v-if="item.resources.containers">{{ item.resources.containers }} 容器 · CPU {{ formatNumber(item.resources.cpuPercent) }}%<small>{{ bytes(item.resources.memoryUsage) }} / {{ bytes(item.resources.memoryLimit) }}</small></template>
                <span v-else class="capability-note">Restricted / Unsupported</span>
              </td>
              <td>
                <template v-if="item.resources.containers">↓ {{ bytes(item.resources.networkReceive) }} / ↑ {{ bytes(item.resources.networkTransmit) }}<small>读 {{ bytes(item.resources.blockRead) }} / 写 {{ bytes(item.resources.blockWrite) }}</small></template>
                <span v-else>Unknown</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!filtered.length" class="inline-empty">没有符合当前筛选条件的应用。</div>
    </section>
  </PageState>
</template>
