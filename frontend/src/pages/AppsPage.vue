<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling } from '@/composables'
import type { ApplicationItem } from '@/types'
import { ago, bytes, formatNumber, percent } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import AppPagination from '@/components/AppPagination.vue'
import BarChart, { type BarItem } from '@/components/BarChart.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'
import SmartSelect, { type SmartOption } from '@/components/SmartSelect.vue'

interface RuntimeUser { id: string; name: string }
interface Payload { items: ApplicationItem[]; users: RuntimeUser[]; source: string; stale: boolean; updatedAt?: string }
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
type ComparisonMetric = 'cpu' | 'memory' | 'network' | 'disk'
interface ComparisonItem { appId: string; deviceId?: string; value: number; unit: string; points: HistoryPoint[] }
interface ComparisonPayload {
  metric: ComparisonMetric
  scope: 'app' | 'instance'
  from: string
  to: string
  bucketSeconds: number
  items: ComparisonItem[]
  updatedAt: string
}

const query = ref(sessionStorage.getItem('watchcatSearch') || '')
const statusFilter = ref('all')
const userFilter = ref('all')
const deviceFilter = ref('all')
const viewMode = ref<'detail' | 'compare'>('detail')
const sortMetric = ref<'cpu' | 'memory' | 'network' | 'disk'>('cpu')
const sortDescending = ref(true)
const selectedAppId = ref('')
const selectedInstanceKey = ref('all')
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
const comparisons = ref<Partial<Record<ComparisonMetric, ComparisonPayload>>>({})
const comparisonLoading = ref(false)
const comparisonError = ref('')
let historyRequest = 0
let comparisonRequest = 0
const { data, loading, error, refresh } = usePolling(() => api<Payload>('/api/v1/applications'))
const paused = computed(() => data.value?.items.reduce((sum, item) => sum + item.paused, 0) ?? 0)
const errors = computed(() => data.value?.items.reduce((sum, item) => sum + item.unhealthy, 0) ?? 0)
const appStatus = (item: ApplicationItem) => item.unhealthy > 0 ? 'critical' : item.paused > 0 ? 'warning' : item.healthy > 0 ? 'healthy' : 'unknown'
const instanceKey = (deviceId: string, deployId: string) => `${deviceId}\u0000${deployId}`
const allInstances = computed(() => (data.value?.items || []).flatMap((application) => application.devices.map((device) => ({ application, device, key: instanceKey(device.deviceId, device.deployId) }))))
const availableDevices = computed(() => {
  const devices = new Map<string, string>()
  for (const item of allInstances.value) devices.set(item.device.deviceId, item.device.deviceName || item.device.deviceId)
  return [...devices].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
})
const availableUsers = computed(() => (data.value?.users || []).filter((user) =>
  deviceFilter.value === 'all' || allInstances.value.some((item) => item.device.deviceId === deviceFilter.value && item.device.userId === user.id)))
const visibleDevices = (item: ApplicationItem) => item.devices.filter((device) =>
  (userFilter.value === 'all' || device.userId === userFilter.value)
  && (deviceFilter.value === 'all' || device.deviceId === deviceFilter.value))
const emptyResources = (): ApplicationItem['resources'] => ({
  containers: 0, cpuPercent: 0, memoryUsage: 0, memoryLimit: 0,
  networkReceive: 0, networkTransmit: 0, blockRead: 0, blockWrite: 0,
})
function mergeResources(left: ApplicationItem['resources'], right: ApplicationItem['resources']) {
  left.containers += right.containers
  left.cpuPercent += right.cpuPercent
  left.memoryUsage += right.memoryUsage
  left.memoryLimit += right.memoryLimit
  left.networkReceive += right.networkReceive
  left.networkTransmit += right.networkTransmit
  left.blockRead += right.blockRead
  left.blockWrite += right.blockWrite
  if (right.updatedAt && (!left.updatedAt || right.updatedAt > left.updatedAt)) left.updatedAt = right.updatedAt
  return left
}
function scopedApplicationResources(item: ApplicationItem) {
  const runningByDevice = new Map<string, ApplicationItem['devices'][number]>()
  for (const device of visibleDevices(item)) {
    if (device.status === 'running' && !runningByDevice.has(device.deviceId)) runningByDevice.set(device.deviceId, device)
  }
  return [...runningByDevice.values()].reduce((total, device) => mergeResources(total, device.resources), emptyResources())
}
const filtered = computed(() => (data.value?.items || []).filter((item) => {
  const matchesQuery = `${item.title} ${item.id}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const status = appStatus(item)
  const matchesStatus = statusFilter.value === 'all'
    || (statusFilter.value === 'healthy' && status === 'healthy')
    || (statusFilter.value === 'degraded' && status === 'warning')
    || (statusFilter.value === 'critical' && status === 'critical')
  const matchesScope = (userFilter.value === 'all' && deviceFilter.value === 'all') || visibleDevices(item).length > 0
  return matchesQuery && matchesStatus && matchesScope
}).sort((a, b) => {
  const delta = applicationSortValue(a) - applicationSortValue(b)
  return (sortDescending.value ? -delta : delta) || (a.title || a.id).localeCompare(b.title || b.id)
}))
const selectedApp = computed(() => data.value?.items.find((item) => item.id === selectedAppId.value))
const selectedInstance = computed(() => selectedApp.value?.devices.find((item) => instanceKey(item.deviceId, item.deployId) === selectedInstanceKey.value))
const activeResources = computed(() => {
  if (selectedInstance.value) return selectedInstance.value.status === 'running' ? selectedInstance.value.resources : emptyResources()
  return selectedApp.value ? scopedApplicationResources(selectedApp.value) : emptyResources()
})
const visibleSelectedDevices = computed(() => selectedApp.value ? visibleDevices(selectedApp.value) : [])
const appPagination = usePagination(filtered, 20)
const instancePagination = usePagination(visibleSelectedDevices, 10)
const selectedInstanceOptions = computed<SmartOption[]>(() => visibleSelectedDevices.value.map((instance) => ({
  value: instanceKey(instance.deviceId, instance.deployId),
  label: instance.userName || instance.userId || instance.deployId,
  group: instance.deviceName || instance.deviceId,
  meta: `${instance.deployId} · ${instance.version || '版本未知'}`,
  status: instance.status,
})))

watch(() => data.value?.items, (items) => {
  if (!items?.length) return
  if (!items.some((item) => item.id === selectedAppId.value)) {
    const preferred = [...items].sort((a, b) => b.resources.cpuPercent - a.resources.cpuPercent)[0]
    selectedAppId.value = preferred.id
  }
}, { immediate: true })
watch(deviceFilter, () => {
  if (userFilter.value !== 'all' && !availableUsers.value.some((item) => item.id === userFilter.value)) userFilter.value = 'all'
  if (selectedInstanceKey.value !== 'all' && !visibleSelectedDevices.value.some((item) => instanceKey(item.deviceId, item.deployId) === selectedInstanceKey.value)) selectedInstanceKey.value = 'all'
  loadCurrentView()
})
watch(userFilter, () => {
  if (selectedInstanceKey.value !== 'all' && !visibleSelectedDevices.value.some((item) => instanceKey(item.deviceId, item.deployId) === selectedInstanceKey.value)) selectedInstanceKey.value = 'all'
  loadCurrentView()
})
watch([query, statusFilter, userFilter, deviceFilter, sortMetric, sortDescending], () => {
  appPagination.resetPage()
  if (filtered.value.length) selectedAppId.value = filtered.value[0].id
})
watch(appPagination.page, () => {
  if (appPagination.pagedItems.value.length) selectedAppId.value = appPagination.pagedItems.value[0].id
})
watch(instancePagination.page, () => {
  selectedInstanceKey.value = 'all'
})
watch(filtered, (items) => {
  if (items.length && !items.some((item) => item.id === selectedAppId.value)) selectedAppId.value = items[0].id
})
watch(selectedAppId, () => {
  instancePagination.resetPage()
  if (selectedInstanceKey.value === 'all') loadHistory()
  else selectedInstanceKey.value = 'all'
})
watch(selectedInstanceKey, loadHistory)
watch(historyHours, () => {
  if (historyMode.value === 'preset') loadCurrentView()
})
async function loadHistory() {
  if (!selectedAppId.value) return
  const request = ++historyRequest
  if (selectedInstance.value && selectedInstance.value.status !== 'running') {
    history.value = undefined
    historyLoading.value = false
    historyError.value = '该实例当前未运行，已隐藏设备级历史，避免误认为数据由该用户实例产生。'
    return
  }
  if (userFilter.value !== 'all' && !selectedInstance.value) {
    history.value = undefined
    historyLoading.value = false
    historyError.value = '容器指标不含用户标签。用户筛选仅用于部署清单，请选择一个运行中的应用实例查看其所在设备的应用数据。'
    return
  }
  historyLoading.value = true
  historyError.value = ''
  try {
    const range = historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
      ? `from=${encodeURIComponent(appliedCustomFrom.value)}&to=${encodeURIComponent(appliedCustomTo.value)}`
      : `hours=${historyHours.value}`
    const scopedDeviceID = selectedInstance.value?.deviceId || (deviceFilter.value !== 'all' ? deviceFilter.value : '')
    const device = scopedDeviceID ? `&deviceId=${encodeURIComponent(scopedDeviceID)}` : ''
    const result = await api<HistoryPayload>(`/api/v1/applications/${encodeURIComponent(selectedAppId.value)}/metrics?${range}${device}`)
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
    const next: Partial<Record<ComparisonMetric, ComparisonPayload>> = {}
    for (const metric of ['cpu', 'memory', 'network', 'disk'] as ComparisonMetric[]) {
      next[metric] = await api<ComparisonPayload>(`/api/v1/applications/metrics/compare?metric=${metric}&scope=instance&${range}`)
      if (request === comparisonRequest) comparisons.value = { ...next }
    }
  } catch (reason) {
    if (request === comparisonRequest) {
      comparisons.value = {}
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
  const resources = scopedApplicationResources(item)
  switch (sortMetric.value) {
    case 'memory': return resources.memoryUsage
    case 'network': return resources.networkReceive + resources.networkTransmit
    case 'disk': return resources.blockRead + resources.blockWrite
    default: return resources.cpuPercent
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
const customHistoryRangeLabel = computed(() => historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
  ? `${new Date(appliedCustomFrom.value).toLocaleString('zh-CN')} 至 ${new Date(appliedCustomTo.value).toLocaleString('zh-CN')}`
  : '')
function comparisonItems(metric: ComparisonMetric) {
  const payload = comparisons.value[metric]
  if (!payload) return []
  const applications = new Map(filtered.value.map((application) => [application.id, application]))
  return payload.items
    .filter((item) => {
      const application = applications.get(item.appId)
      return application && visibleDevices(application).some((device) => device.deviceId === item.deviceId)
    })
    .map((item) => {
      const application = applications.get(item.appId)!
      const device = application.devices.find((candidate) => candidate.deviceId === item.deviceId)
      return {
        ...item,
        title: `${application.title || application.id} / ${device?.deviceName || item.deviceId}`,
        deviceName: device?.deviceName || item.deviceId || '未知设备',
      }
    })
    .sort((a, b) => (sortDescending.value ? b.value - a.value : a.value - b.value) || a.title.localeCompare(b.title))
}
const comparisonGroups = computed<Array<{ metric: ComparisonMetric; title: string; unit: string; color: string; loaded: boolean; items: BarItem[] }>>(() => {
  const definitions: Array<{ metric: ComparisonMetric; title: string; unit: string; color: string; scale: number }> = [
    { metric: 'cpu', title: '所有应用 CPU', unit: '%', color: '#2563eb', scale: 1 },
    { metric: 'memory', title: '所有应用内存', unit: ' MiB', color: '#7c3aed', scale: 1024 ** 2 },
    { metric: 'network', title: '所有应用网络流量', unit: ' MiB', color: '#15803d', scale: 1024 ** 2 },
    { metric: 'disk', title: '所有应用磁盘 I/O', unit: ' MiB', color: '#c05600', scale: 1024 ** 2 },
  ]
  return definitions.map((definition) => ({
    ...definition,
    loaded: Boolean(comparisons.value[definition.metric]),
    items: comparisonItems(definition.metric).map((item) => ({
      label: item.title,
      value: Number((item.value / definition.scale).toFixed(item.value / definition.scale >= 10 ? 1 : 2)),
      color: definition.color,
      hint: `${item.appId} · ${item.deviceName} · ${item.points.length ? (definition.metric === 'cpu' ? `${formatNumber(item.value)}%` : bytes(item.value)) : '当前时间范围无历史数据'}`,
    })),
  }))
})
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
        <select v-model="userFilter" aria-label="实例用户"><option value="all">全部用户</option><option v-for="user in availableUsers" :key="user.id" :value="user.id">{{ user.name || user.id }}</option></select>
        <select v-model="deviceFilter" aria-label="应用设备"><option value="all">全部设备</option><option v-for="device in availableDevices" :key="device.id" :value="device.id">{{ device.name }}</option></select>
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
          <button v-for="item in appPagination.pagedItems.value" :key="item.id" :class="['app-resource-item', { active: selectedAppId === item.id }]" @click="selectedAppId = item.id">
            <i :class="appStatus(item)" />
            <span><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small></span>
            <span class="app-resource-now"><b>{{ formatNumber(scopedApplicationResources(item).cpuPercent) }}%</b><small>{{ bytes(scopedApplicationResources(item).memoryUsage) }}</small></span>
          </button>
        </div>
        <div v-else class="inline-empty">没有符合当前筛选条件的应用。</div>
        <AppPagination v-model:page="appPagination.page.value" v-model:page-size="appPagination.pageSize.value" :total="appPagination.total.value" :page-count="appPagination.pageCount.value" :range-start="appPagination.rangeStart.value" :range-end="appPagination.rangeEnd.value" label="应用列表分页" />
      </aside>

      <main v-if="selectedApp" class="app-resource-detail">
        <section class="card app-resource-hero">
          <div class="section-title app-resource-heading">
            <div><h2>{{ selectedApp.title || selectedApp.id }}</h2><span class="muted">{{ selectedApp.id }} · {{ Object.keys(selectedApp.versions).join(' / ') || '版本未知' }}</span></div>
            <div class="app-instance-switcher">
              <label for="application-instance">应用实例</label>
              <SmartSelect v-model="selectedInstanceKey" :options="selectedInstanceOptions" :all-label="`全部实例（${visibleSelectedDevices.length}）`" control-label="应用实例" searchable />
              <StatusPill :status="selectedInstance ? (selectedInstance.status === 'running' ? 'healthy' : selectedInstance.status === 'error' ? 'critical' : 'warning') : appStatus(selectedApp)" />
            </div>
          </div>
          <div class="app-resource-kpis">
            <div><span>当前 CPU</span><strong>{{ formatNumber(activeResources?.cpuPercent ?? 0) }}%</strong><small>{{ activeResources?.containers ?? 0 }} 个容器</small></div>
            <div><span>当前内存</span><strong>{{ bytes(activeResources?.memoryUsage ?? 0) }}</strong><small>{{ percent(activeResources?.memoryUsage ?? 0, activeResources?.memoryLimit ?? 0) }} 配额</small></div>
            <div><span>区间流量总和</span><strong>{{ bytes(history?.summary?.networkTotalBytes ?? 0) }}</strong><small>接收 {{ bytes(history?.summary?.networkReceiveRateBytes ?? 0) }} · 发送 {{ bytes(history?.summary?.networkTransmitRateBytes ?? 0) }}</small></div>
            <div><span>区间磁盘 IO</span><strong>{{ bytes(history?.summary?.blockTotalBytes ?? 0) }}</strong><small>读取 {{ bytes(history?.summary?.blockReadRateBytes ?? 0) }} · 写入 {{ bytes(history?.summary?.blockWriteRateBytes ?? 0) }}</small></div>
          </div>
          <p v-if="selectedInstance || userFilter !== 'all'" class="operation-evidence">
            容器指标按“设备 + 应用”采集；同一设备存在多个同应用用户实例时，无法按用户部署 ID 拆分。未运行实例不展示实时数据。
          </p>
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
          <div class="section-title compact"><div><h2>应用实例</h2></div></div>
          <div class="table-scroll">
            <table class="fleet-table app-instance-table">
              <thead><tr><th>设备</th><th>用户</th><th>状态</th><th>版本</th><th>部署 ID</th><th>更新时间</th></tr></thead>
              <tbody><tr v-for="instance in instancePagination.pagedItems.value" :key="instanceKey(instance.deviceId, instance.deployId)" :class="{ selected: selectedInstanceKey === instanceKey(instance.deviceId, instance.deployId) }" tabindex="0" @click="selectedInstanceKey = instanceKey(instance.deviceId, instance.deployId)" @keydown.enter="selectedInstanceKey = instanceKey(instance.deviceId, instance.deployId)">
                <td><button class="row-link" @click.stop="selectedInstanceKey = instanceKey(instance.deviceId, instance.deployId)">{{ instance.deviceName || instance.deviceId }}</button></td><td>{{ instance.userName || instance.userId || '未知' }}</td><td><StatusPill :status="instance.status === 'running' ? 'healthy' : instance.status === 'error' ? 'critical' : 'warning'" /></td><td>{{ instance.version || '未知' }}</td><td><code>{{ instance.deployId }}</code></td><td>{{ ago(instance.collectedAt) }}</td>
              </tr></tbody>
            </table>
          </div>
          <AppPagination v-model:page="instancePagination.page.value" v-model:page-size="instancePagination.pageSize.value" :total="instancePagination.total.value" :page-count="instancePagination.pageCount.value" :range-start="instancePagination.rangeStart.value" :range-end="instancePagination.rangeEnd.value" label="运行实例分页" />
        </section>
      </main>
    </div>

    <section v-else class="card app-comparison-card">
      <div class="section-title"><div><h2>所有应用对比</h2></div></div>
      <p v-if="userFilter !== 'all'" class="operation-evidence warning">用户筛选只限定应用部署关系；历史指标仍按“设备 + 应用”聚合，不代表该用户独占这些资源。</p>
      <p v-if="comparisonLoading" class="operation-evidence">正在依次计算 CPU、内存、网络流量和磁盘 I/O…</p>
      <div v-if="comparisonError" class="inline-empty">对比数据加载失败：{{ comparisonError }} <button class="row-link" @click="loadComparison">重试</button></div>
      <template v-else>
        <div class="all-app-metric-grid">
          <section v-for="group in comparisonGroups" :key="group.metric" class="all-app-metric-panel">
            <div class="section-title compact"><div><h3>{{ group.title }}</h3></div><span class="pill unknown">{{ group.items.length }} 个应用设备组合</span></div>
            <div v-if="!group.loaded" class="metric-panel-loading">正在计算…</div>
            <BarChart v-else :items="group.items" :unit="group.unit" />
          </section>
        </div>
        <div v-if="!comparisonLoading && comparisonGroups.every((group) => !group.items.length)" class="inline-empty">当前时间范围内没有可对比的应用指标。</div>
      </template>
    </section>
  </PageState>
</template>
