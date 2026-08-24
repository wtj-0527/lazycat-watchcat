<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { ApplicationItem } from '@/types'
import { ago, bytes, formatNumber, percent } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: ApplicationItem[]; source: string; stale: boolean; updatedAt?: string }
interface HistoryPoint { value: number; collectedAt: string }
interface HistoryPayload {
  appId: string
  hours: number
  bucketSeconds: number
  updatedAt: string
  series: {
    cpuPercent: HistoryPoint[]
    memoryUsage: HistoryPoint[]
    networkReceiveRate: HistoryPoint[]
    networkTransmitRate: HistoryPoint[]
    blockReadRate: HistoryPoint[]
    blockWriteRate: HistoryPoint[]
  }
}

const query = ref(sessionStorage.getItem('maoyanSearch') || '')
const statusFilter = ref('all')
const selectedAppId = ref('')
const historyHours = ref(24)
const history = ref<HistoryPayload>()
const historyLoading = ref(false)
const historyError = ref('')
let historyRequest = 0
const { data, loading, error, refresh } = usePolling(() => api<Payload>('/api/v1/applications'))
const paused = computed(() => data.value?.items.reduce((sum, item) => sum + item.paused, 0) ?? 0)
const errors = computed(() => data.value?.items.reduce((sum, item) => sum + item.unhealthy, 0) ?? 0)
const appStatus = (item: ApplicationItem) => item.unhealthy > 0 ? 'critical' : item.paused > 0 ? 'warning' : item.healthy > 0 ? 'healthy' : 'unknown'
const filtered = computed(() => (data.value?.items || []).filter((item) => {
  const matchesQuery = `${item.title} ${item.id}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const status = appStatus(item)
  const matchesStatus = statusFilter.value === 'all'
    || (statusFilter.value === 'healthy' && status === 'healthy')
    || (statusFilter.value === 'degraded' && status === 'warning')
    || (statusFilter.value === 'critical' && status === 'critical')
  return matchesQuery && matchesStatus
}).sort((a, b) => {
  const rank = ({ critical: 0, warning: 1, healthy: 2, unknown: 3 } as Record<string, number>)
  return rank[appStatus(a)] - rank[appStatus(b)] || (a.title || a.id).localeCompare(b.title || b.id)
}))
const selectedApp = computed(() => data.value?.items.find((item) => item.id === selectedAppId.value))

watch(() => data.value?.items, (items) => {
  if (!items?.length) return
  if (!items.some((item) => item.id === selectedAppId.value)) {
    const preferred = [...items].sort((a, b) => b.resources.cpuPercent - a.resources.cpuPercent)[0]
    selectedAppId.value = preferred.id
  }
}, { immediate: true })
watch([selectedAppId, historyHours], loadHistory)

async function loadHistory() {
  if (!selectedAppId.value) return
  const request = ++historyRequest
  historyLoading.value = true
  historyError.value = ''
  try {
    const result = await api<HistoryPayload>(`/api/v1/applications/${encodeURIComponent(selectedAppId.value)}/metrics?hours=${historyHours.value}`)
    if (request === historyRequest) history.value = result
  } catch (reason) {
    if (request === historyRequest) {
      history.value = undefined
      historyError.value = reason instanceof Error ? reason.message : String(reason)
    }
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}
function chartPoints(items: HistoryPoint[] | undefined, scale = 1) {
  return (items || []).map((item) => ({
    value: item.value / scale,
    at: new Date(item.collectedAt).toLocaleString('zh-CN'),
    label: new Date(item.collectedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
  }))
}
const cpuSeries = computed<ChartSeries[]>(() => [{ name: 'CPU', color: '#2563eb', points: chartPoints(history.value?.series.cpuPercent) }])
const memorySeries = computed<ChartSeries[]>(() => [{ name: '内存', color: '#7c3aed', points: chartPoints(history.value?.series.memoryUsage, 1024 * 1024) }])
const networkSeries = computed<ChartSeries[]>(() => [
  { name: '接收', color: '#15803d', points: chartPoints(history.value?.series.networkReceiveRate, 1024) },
  { name: '发送', color: '#c05600', points: chartPoints(history.value?.series.networkTransmitRate, 1024) },
])
const blockSeries = computed<ChartSeries[]>(() => [
  { name: '读取', color: '#2563eb', points: chartPoints(history.value?.series.blockReadRate, 1024) },
  { name: '写入', color: '#c51d23', points: chartPoints(history.value?.series.blockWriteRate, 1024) },
])
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="LazyCat Package Manager 尚未返回当前用户的应用状态。" @retry="refresh">
    <div class="page-intro app-resource-intro">
      <div><h2>应用资源</h2></div>
      <span class="muted">更新 {{ ago(data?.updatedAt) }}</span>
    </div>
    <div class="filter-bar app-filter-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索应用名称"></label>
      <select v-model="statusFilter" aria-label="应用状态"><option value="all">全部状态</option><option value="healthy">运行正常</option><option value="degraded">已暂停</option><option value="critical">异常</option></select>
      <span class="pill critical">异常 {{ errors }}</span><span class="pill warning">已暂停 {{ paused }}</span>
      <span v-if="data?.stale" class="stale-banner">Runtime 状态已陈旧</span>
    </div>

    <div class="app-resource-layout">
      <aside class="card app-resource-list-card">
        <div class="section-title compact"><div><h2>应用</h2><span class="muted">{{ filtered.length }} 个结果</span></div></div>
        <div v-if="filtered.length" class="app-resource-list">
          <button v-for="item in filtered" :key="item.id" :class="['app-resource-item', { active: selectedAppId === item.id }]" @click="selectedAppId = item.id">
            <i :class="appStatus(item)" />
            <span><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small></span>
            <span class="app-resource-now"><b>{{ formatNumber(item.resources.cpuPercent) }}%</b><small>{{ bytes(item.resources.memoryUsage) }}</small></span>
          </button>
        </div>
        <div v-else class="inline-empty">没有符合当前筛选条件的应用。</div>
      </aside>

      <main v-if="selectedApp" class="app-resource-detail">
        <section class="card app-resource-hero">
          <div class="section-title">
            <div><h2>{{ selectedApp.title || selectedApp.id }}</h2><span class="muted">{{ selectedApp.id }} · {{ Object.keys(selectedApp.versions).join(' / ') || '版本未知' }}</span></div>
            <StatusPill :status="appStatus(selectedApp)" />
          </div>
          <div class="app-resource-kpis">
            <div><span>当前 CPU</span><strong>{{ formatNumber(selectedApp.resources.cpuPercent) }}%</strong><small>{{ selectedApp.resources.containers }} 个容器</small></div>
            <div><span>当前内存</span><strong>{{ bytes(selectedApp.resources.memoryUsage) }}</strong><small>{{ percent(selectedApp.resources.memoryUsage, selectedApp.resources.memoryLimit) }} 配额</small></div>
            <div><span>累计网络</span><strong>{{ bytes(selectedApp.resources.networkReceive) }}</strong><small>发送 {{ bytes(selectedApp.resources.networkTransmit) }}</small></div>
            <div><span>累计磁盘 IO</span><strong>{{ bytes(selectedApp.resources.blockRead) }}</strong><small>写入 {{ bytes(selectedApp.resources.blockWrite) }}</small></div>
          </div>
        </section>

        <section class="card app-history-card">
          <div class="section-title">
            <div><h2>资源历史</h2><span class="muted">CPU、内存、网络吞吐和磁盘 IO 均来自容器历史指标</span></div>
            <div class="history-range" aria-label="历史时间范围">
              <button v-for="item in [1, 6, 24]" :key="item" :class="{ active: historyHours === item }" @click="historyHours = item">{{ item }} 小时</button>
            </div>
          </div>
          <div v-if="historyLoading" class="history-loading">正在读取历史指标…</div>
          <div v-else-if="historyError" class="inline-empty">历史指标加载失败：{{ historyError }} <button class="row-link" @click="loadHistory">重试</button></div>
          <div v-else class="app-history-grid">
            <div><h3>CPU 使用率</h3><LineChart :series="cpuSeries" :min="0" unit="%" :height="220" /></div>
            <div><h3>内存使用量</h3><LineChart :series="memorySeries" :min="0" unit=" MiB" :height="220" /></div>
            <div><h3>网络吞吐</h3><LineChart :series="networkSeries" :min="0" unit=" KiB/s" :height="220" /></div>
            <div><h3>磁盘 IO</h3><LineChart :series="blockSeries" :min="0" unit=" KiB/s" :height="220" /></div>
          </div>
        </section>

        <section class="card">
          <div class="section-title compact"><div><h2>运行实例</h2><span class="muted">当前 Package Manager 状态</span></div></div>
          <div class="table-scroll">
            <table class="fleet-table app-instance-table">
              <thead><tr><th>设备</th><th>状态</th><th>版本</th><th>部署 ID</th><th>更新时间</th></tr></thead>
              <tbody><tr v-for="instance in selectedApp.devices" :key="instance.deployId">
                <td><b>{{ instance.deviceName || instance.deviceId }}</b></td><td><StatusPill :status="instance.status === 'running' ? 'healthy' : instance.status === 'error' ? 'critical' : 'warning'" /></td><td>{{ instance.version || '未知' }}</td><td><code>{{ instance.deployId }}</code></td><td>{{ ago(instance.collectedAt) }}</td>
              </tr></tbody>
            </table>
          </div>
        </section>
      </main>
    </div>
  </PageState>
</template>
