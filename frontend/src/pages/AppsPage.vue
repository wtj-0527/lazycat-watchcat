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
  from: string
  to: string
  bucketSeconds: number
  updatedAt: string
  summary: {
    networkReceiveRateBytes: number
    networkTransmitRateBytes: number
    networkTotalBytes: number
    blockReadRateBytes: number
    blockWriteRateBytes: number
    blockTotalBytes: number
  }
  series: {
    cpuPercent: HistoryPoint[]
    memoryUsage: HistoryPoint[]
    networkReceiveRate: HistoryPoint[]
    networkTransmitRate: HistoryPoint[]
    blockReadRate: HistoryPoint[]
    blockWriteRate: HistoryPoint[]
  }
}
interface ComparisonItem { appId: string; value: number; unit: string; points: HistoryPoint[] }
interface ComparisonPayload {
  metric: 'cpu' | 'memory' | 'network' | 'disk'
  from: string
  to: string
  bucketSeconds: number
  items: ComparisonItem[]
  updatedAt: string
}

const query = ref(sessionStorage.getItem('maoyanSearch') || '')
const statusFilter = ref('all')
const viewMode = ref<'detail' | 'compare'>('detail')
const sortMetric = ref<'cpu' | 'memory' | 'network' | 'disk'>('cpu')
const sortDescending = ref(true)
const selectedAppId = ref('')
const historyHours = ref(24)
const historyMode = ref<'preset' | 'custom'>('preset')
const showCustomRange = ref(false)
const customFrom = ref(toLocalDateTime(new Date(Date.now() - 24 * 60 * 60 * 1000)))
const customTo = ref(toLocalDateTime(new Date()))
const appliedCustomFrom = ref('')
const appliedCustomTo = ref('')
const customRangeError = ref('')
const history = ref<HistoryPayload>()
const historyLoading = ref(false)
const historyError = ref('')
const comparison = ref<ComparisonPayload>()
const comparisonLoading = ref(false)
const comparisonError = ref('')
let historyRequest = 0
let comparisonRequest = 0
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
  const delta = applicationSortValue(a) - applicationSortValue(b)
  return (sortDescending.value ? -delta : delta) || (a.title || a.id).localeCompare(b.title || b.id)
}))
const selectedApp = computed(() => data.value?.items.find((item) => item.id === selectedAppId.value))

watch(() => data.value?.items, (items) => {
  if (!items?.length) return
  if (!items.some((item) => item.id === selectedAppId.value)) {
    const preferred = [...items].sort((a, b) => b.resources.cpuPercent - a.resources.cpuPercent)[0]
    selectedAppId.value = preferred.id
  }
}, { immediate: true })
watch(selectedAppId, loadHistory)
watch(historyHours, () => {
  if (historyMode.value === 'preset') loadCurrentView()
})
watch(sortMetric, () => {
  if (viewMode.value === 'compare') loadComparison()
})

async function loadHistory() {
  if (!selectedAppId.value) return
  const request = ++historyRequest
  historyLoading.value = true
  historyError.value = ''
  try {
    const range = historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
      ? `from=${encodeURIComponent(appliedCustomFrom.value)}&to=${encodeURIComponent(appliedCustomTo.value)}`
      : `hours=${historyHours.value}`
    const result = await api<HistoryPayload>(`/api/v1/applications/${encodeURIComponent(selectedAppId.value)}/metrics?${range}`)
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
async function loadComparison() {
  const request = ++comparisonRequest
  comparisonLoading.value = true
  comparisonError.value = ''
  try {
    const range = historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
      ? `from=${encodeURIComponent(appliedCustomFrom.value)}&to=${encodeURIComponent(appliedCustomTo.value)}`
      : `hours=${historyHours.value}`
    const result = await api<ComparisonPayload>(`/api/v1/applications/metrics/compare?metric=${sortMetric.value}&${range}`)
    if (request === comparisonRequest) comparison.value = result
  } catch (reason) {
    if (request === comparisonRequest) {
      comparison.value = undefined
      comparisonError.value = reason instanceof Error ? reason.message : String(reason)
    }
  } finally {
    if (request === comparisonRequest) comparisonLoading.value = false
  }
}
function loadCurrentView() {
  if (viewMode.value === 'compare') loadComparison()
  else loadHistory()
}
function setViewMode(mode: 'detail' | 'compare') {
  viewMode.value = mode
  loadCurrentView()
}
function applicationSortValue(item: ApplicationItem) {
  switch (sortMetric.value) {
    case 'memory': return item.resources.memoryUsage
    case 'network': return item.resources.networkReceive + item.resources.networkTransmit
    case 'disk': return item.resources.blockRead + item.resources.blockWrite
    default: return item.resources.cpuPercent
  }
}
function toLocalDateTime(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}
function selectPreset(hours: number) {
  historyMode.value = 'preset'
  showCustomRange.value = false
  customRangeError.value = ''
  if (historyHours.value === hours) loadCurrentView()
  else historyHours.value = hours
}
function applyCustomRange() {
  const from = new Date(customFrom.value)
  const to = new Date(customTo.value)
  if (!customFrom.value || !customTo.value || Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) {
    customRangeError.value = '请选择完整的开始和结束时间。'
    return
  }
  if (from >= to) {
    customRangeError.value = '开始时间必须早于结束时间。'
    return
  }
  if (to.getTime() - from.getTime() > 30 * 24 * 60 * 60 * 1000) {
    customRangeError.value = '单次查询范围不能超过 30 天。'
    return
  }
  customRangeError.value = ''
  appliedCustomFrom.value = from.toISOString()
  appliedCustomTo.value = to.toISOString()
  historyMode.value = 'custom'
  showCustomRange.value = false
  loadCurrentView()
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
const comparisonScale = computed(() => sortMetric.value === 'memory' ? 1024 * 1024 : sortMetric.value === 'network' || sortMetric.value === 'disk' ? 1024 : 1)
const comparisonUnit = computed(() => ({ cpu: '%', memory: ' MiB', network: ' KiB/s', disk: ' KiB/s' } as const)[sortMetric.value])
const comparisonLabel = computed(() => ({ cpu: 'CPU 平均使用率', memory: '内存平均使用量', network: '网络流量总和', disk: '磁盘 IO 总和' } as const)[sortMetric.value])
const customHistoryRangeLabel = computed(() => historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
  ? `${new Date(appliedCustomFrom.value).toLocaleString('zh-CN')} 至 ${new Date(appliedCustomTo.value).toLocaleString('zh-CN')}`
  : '')
const comparisonItems = computed(() => {
  const names = new Map((data.value?.items || []).map((item) => [item.id, item.title || item.id]))
  const visible = new Set(filtered.value.map((item) => item.id))
  return [...(comparison.value?.items || [])]
    .filter((item) => visible.has(item.appId))
    .map((item) => ({ ...item, title: names.get(item.appId) || item.appId }))
    .sort((a, b) => (sortDescending.value ? b.value - a.value : a.value - b.value))
})
const comparisonSeries = computed<ChartSeries[]>(() => {
  const colors = ['#2563eb', '#c51d23', '#15803d', '#c05600', '#7c3aed', '#0891b2', '#db2777', '#475569']
  return comparisonItems.value.slice(0, 8).map((item, index) => ({
    name: item.title,
    color: colors[index],
    points: chartPoints(item.points, comparisonScale.value),
  }))
})
function comparisonValue(item: ComparisonItem) {
  if (sortMetric.value === 'cpu') return `${formatNumber(item.value)}%`
  if (sortMetric.value === 'memory') return bytes(item.value)
  return bytes(item.value)
}
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="LazyCat Package Manager 尚未返回当前用户的应用状态。" @retry="refresh">
    <div class="page-intro app-resource-intro">
      <div><h2>应用资源</h2></div>
      <div class="view-toggle"><button :class="{ active: viewMode === 'detail' }" @click="setViewMode('detail')">单应用</button><button :class="{ active: viewMode === 'compare' }" @click="setViewMode('compare')">全部应用对比</button></div>
    </div>
    <section class="card application-controls">
      <div class="filter-bar app-filter-bar">
        <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索应用名称"></label>
        <select v-model="statusFilter" aria-label="应用状态"><option value="all">全部状态</option><option value="healthy">运行正常</option><option value="degraded">已暂停</option><option value="critical">异常</option></select>
        <select v-model="sortMetric" aria-label="排序指标"><option value="cpu">按 CPU 排序</option><option value="memory">按内存排序</option><option value="network">按网络流量排序</option><option value="disk">按磁盘 IO 排序</option></select>
        <button class="secondary-button sort-direction" :aria-label="sortDescending ? '当前降序，点击切换升序' : '当前升序，点击切换降序'" @click="sortDescending = !sortDescending">{{ sortDescending ? '从高到低 ↓' : '从低到高 ↑' }}</button>
        <span class="pill critical">异常 {{ errors }}</span><span class="pill warning">已暂停 {{ paused }}</span>
        <span class="filter-note">30 秒自动刷新</span>
      </div>
      <div class="application-time-controls">
        <div><b>统计时间</b><span v-if="customHistoryRangeLabel" class="muted">{{ customHistoryRangeLabel }}</span></div>
        <div class="history-range" aria-label="历史时间范围">
          <button v-for="item in [1, 6, 24]" :key="item" :class="{ active: historyMode === 'preset' && historyHours === item }" @click="selectPreset(item)">{{ item }} 小时</button>
          <button :class="{ active: historyMode === 'custom' }" @click="showCustomRange = !showCustomRange">自定义</button>
        </div>
        <div v-if="showCustomRange" class="custom-history-range">
          <label><span>开始时间</span><input v-model="customFrom" type="datetime-local"></label>
          <label><span>结束时间</span><input v-model="customTo" type="datetime-local"></label>
          <button class="primary-button" @click="applyCustomRange">应用时间范围</button>
          <button class="secondary-button" @click="showCustomRange = false">取消</button>
          <small v-if="customRangeError">{{ customRangeError }}</small>
        </div>
      </div>
    </section>

    <div v-if="viewMode === 'detail'" class="app-resource-layout">
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
            <div><span>区间流量总和</span><strong>{{ bytes(history?.summary?.networkTotalBytes ?? 0) }}</strong><small>接收 {{ bytes(history?.summary?.networkReceiveRateBytes ?? 0) }} · 发送 {{ bytes(history?.summary?.networkTransmitRateBytes ?? 0) }}</small></div>
            <div><span>区间磁盘 IO</span><strong>{{ bytes(history?.summary?.blockTotalBytes ?? 0) }}</strong><small>读取 {{ bytes(history?.summary?.blockReadRateBytes ?? 0) }} · 写入 {{ bytes(history?.summary?.blockWriteRateBytes ?? 0) }}</small></div>
          </div>
        </section>

        <section class="card app-history-card">
          <div class="section-title"><div><h2>资源历史</h2></div></div>
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
          <div class="section-title compact"><div><h2>运行实例</h2></div></div>
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

    <section v-else class="card app-comparison-card">
      <div class="section-title">
        <div><h2>所有应用对比</h2><span class="muted">{{ comparisonLabel }} · 图表展示前 8 名，表格包含全部有历史数据的应用</span></div>
      </div>
      <div v-if="comparisonLoading" class="history-loading">正在计算所有应用对比数据…</div>
      <div v-else-if="comparisonError" class="inline-empty">对比数据加载失败：{{ comparisonError }} <button class="row-link" @click="loadComparison">重试</button></div>
      <template v-else>
        <div class="comparison-chart"><LineChart :series="comparisonSeries" :min="0" :unit="comparisonUnit" :height="280" /></div>
        <div class="table-scroll">
          <table class="fleet-table app-comparison-table">
            <thead><tr><th>排名</th><th>应用</th><th>{{ comparisonLabel }}</th><th>历史点数</th></tr></thead>
            <tbody><tr v-for="(item, index) in comparisonItems" :key="item.appId">
              <td>{{ index + 1 }}</td><td><b>{{ item.title }}</b><small>{{ item.appId }}</small></td><td><strong>{{ comparisonValue(item) }}</strong></td><td>{{ item.points.length }}</td>
            </tr></tbody>
          </table>
        </div>
        <div v-if="!comparisonItems.length" class="inline-empty">当前时间范围内没有可对比的应用指标。</div>
      </template>
    </section>
  </PageState>
</template>
