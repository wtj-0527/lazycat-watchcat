<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling } from '@/composables'
import type { ApplicationItem } from '@/types'
import { bytes, dateTime, formatNumber, parseBeijingDateTimeInput, percent, timeOfDay, toBeijingDateTimeInput } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import AppPagination from '@/components/AppPagination.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'
import SmartSelect, { type SmartOption } from '@/components/SmartSelect.vue'
import { appConfirm } from '@/dialog'
import { globalRealtime } from '@/realtime'
import { metricColors } from '@/metricColors'
import { globalDeviceId } from '@/deviceScope'

interface RuntimeUserPolicy { deviceId: string; appAccessNoLimit: boolean; allowedAppIds: string[] }
interface RuntimeUser { id: string; name: string; policies?: RuntimeUserPolicy[] }
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
interface ComparisonItem { appId: string; deviceId?: string; deployId?: string; userId?: string; value: number; unit: string; points: HistoryPoint[] }
interface ComparisonPayload {
  metric: ComparisonMetric
  scope: 'app' | 'instance'
  from: string
  to: string
  bucketSeconds: number
  items: ComparisonItem[]
  updatedAt: string
}
interface ApplicationOperation {
  id?: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  error?: string
  instanceStatus?: string
  autostart?: boolean | null
}

const emit = defineEmits<{ toast: [message: string] }>()

const query = ref(sessionStorage.getItem('watchcatSearch') || '')
const statusFilter = ref('all')
const userFilter = ref('all')
const deviceFilter = globalDeviceId
const viewMode = ref<'detail' | 'compare'>('detail')
const sortMetric = ref<'name' | 'cpu' | 'memory' | 'network' | 'disk'>('cpu')
const sortDescending = ref(true)
const selectedAppId = ref('')
const selectedInstanceKey = ref('all')
const historyHours = ref(24)
const historyMode = ref<'preset' | 'custom'>('preset')
const showCustomRange = ref(false)
const customFrom = ref(toBeijingDateTimeInput(new Date(Date.now() - 24 * 60 * 60 * 1000)))
const customTo = ref(toBeijingDateTimeInput(new Date()))
const appliedCustomFrom = ref('')
const appliedCustomTo = ref('')
const customRangeError = ref('')
const history = ref<HistoryPayload>()
const historyLoading = ref(false)
const historyError = ref('')
const comparisons = ref<Partial<Record<ComparisonMetric, ComparisonPayload>>>({})
const comparisonLoading = ref(false)
const comparisonError = ref('')
const comparisonInstanceKey = ref('all')
const instanceAction = ref('')
let historyRequest = 0
let comparisonRequest = 0
const { data, loading, error, refresh } = usePolling(() => api<Payload>('/api/v1/applications'))
const paused = computed(() => data.value?.items.reduce((sum, item) => sum + item.paused, 0) ?? 0)
const errors = computed(() => data.value?.items.reduce((sum, item) => sum + item.unhealthy, 0) ?? 0)
const appStatus = (item: ApplicationItem) => item.unhealthy > 0 ? 'critical' : item.paused > 0 ? 'warning' : item.healthy > 0 ? 'healthy' : 'unknown'
const applicationRuntimeLabel = (item: ApplicationItem) =>
  item.unhealthy > 0 ? '存在异常' : item.paused > 0 && item.healthy > 0 ? '部分暂停' : item.paused > 0 ? '已暂停' : item.healthy > 0 ? '运行中' : '状态未知'
const runtimeStatusTone = (status: string) => status === 'running' ? 'healthy' : status === 'error' ? 'critical' : status === 'paused' ? 'warning' : 'unknown'
const runtimeStatusLabel = (status: string) => status === 'running' ? '运行中' : status === 'paused' ? '已暂停' : status === 'error' ? '异常' : status || '未知'
const instanceKey = (deviceId: string, deployId: string) => `${deviceId}\u0000${deployId}`
const allInstances = computed(() => (data.value?.items || []).flatMap((application) => application.devices.map((device) => ({ application, device, key: instanceKey(device.deviceId, device.deployId) }))))
const availableDevices = computed(() => {
  const devices = new Map<string, string>()
  for (const item of allInstances.value) devices.set(item.device.deviceId, item.device.deviceName || item.device.deviceId)
  return [...devices].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
})
const availableUsers = computed(() => (data.value?.users || []).filter((user) =>
  deviceFilter.value === 'all'
  || user.policies?.some((policy) => policy.deviceId === deviceFilter.value)
  || allInstances.value.some((item) => item.device.deviceId === deviceFilter.value && item.device.userId === user.id)))
const selectedUserPolicies = computed(() => {
  const user = (data.value?.users || []).find((item) => item.id === userFilter.value)
  return (user?.policies || []).filter((policy) => deviceFilter.value === 'all' || policy.deviceId === deviceFilter.value)
})
const implicitAccessAppIDs = new Set(['cloud.lazycat.shell.appstore', 'cloud.lazycat.shell.settings'])
const userCanAccessApp = (item: ApplicationItem) => {
  if (userFilter.value === 'all') return true
  if (implicitAccessAppIDs.has(item.id)) return false
  if (selectedUserPolicies.value.length) {
    return selectedUserPolicies.value.some((policy) => policy.appAccessNoLimit || policy.allowedAppIds.includes(item.id))
  }
  return item.devices.some((device) =>
    device.userId === userFilter.value
    && (deviceFilter.value === 'all' || device.deviceId === deviceFilter.value)
    && device.accessPolicyKnown && device.accessGranted)
}
const visibleDevices = (item: ApplicationItem) => item.devices.filter((device) =>
  (userFilter.value === 'all' || device.userId === userFilter.value)
  && (userFilter.value === 'all' || device.accessPolicyKnown && device.accessGranted)
  && (deviceFilter.value === 'all' || device.deviceId === deviceFilter.value))
const scopedRuntimeState = (item: ApplicationItem) => {
  if (userFilter.value === 'all') return { tone: appStatus(item), label: applicationRuntimeLabel(item) }
  const instances = visibleDevices(item)
  if (!instances.length) return { tone: 'unknown', label: '暂无独立实例' }
  if (instances.some((device) => device.status === 'error')) return { tone: 'critical', label: '存在异常' }
  if (instances.some((device) => device.status === 'running') && instances.some((device) => device.status === 'paused')) return { tone: 'warning', label: '部分暂停' }
  if (instances.some((device) => device.status === 'running')) return { tone: 'healthy', label: '运行中' }
  if (instances.some((device) => device.status === 'paused')) return { tone: 'warning', label: '已暂停' }
  return { tone: 'unknown', label: '状态未知' }
}
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
  const runningInstances = new Map<string, ApplicationItem['devices'][number]>()
  for (const device of visibleDevices(item)) {
    if (device.status === 'running') runningInstances.set(instanceKey(device.deviceId, device.deployId), device)
  }
  return [...runningInstances.values()].reduce((total, device) => mergeResources(total, device.resources), emptyResources())
}
const filtered = computed(() => (data.value?.items || []).filter((item) => {
  const matchesQuery = `${item.title} ${item.id}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const status = scopedRuntimeState(item).tone
  const matchesStatus = statusFilter.value === 'all'
    || (statusFilter.value === 'healthy' && status === 'healthy')
    || (statusFilter.value === 'degraded' && status === 'warning')
    || (statusFilter.value === 'critical' && status === 'critical')
  const matchesDevice = deviceFilter.value === 'all' || item.devices.some((device) => device.deviceId === deviceFilter.value)
  const matchesScope = userCanAccessApp(item) && matchesDevice
  return matchesQuery && matchesStatus && matchesScope
}).sort((a, b) => {
  if (sortMetric.value === 'name') {
    const delta = (a.title || a.id).localeCompare(b.title || b.id)
    return sortDescending.value ? -delta : delta
  }
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
const sortedVisibleSelectedDevices = computed(() => [...visibleSelectedDevices.value].sort((left, right) => {
  const statusOrder = (status: string) => status === 'paused' ? 1 : 0
  return statusOrder(left.status) - statusOrder(right.status)
    || (left.deviceName || left.deviceId).localeCompare(right.deviceName || right.deviceId)
    || (left.userName || left.userId || left.deployId).localeCompare(right.userName || right.userId || right.deployId)
}))
const appPagination = usePagination(filtered, 20)
const selectedInstanceOptions = computed<SmartOption[]>(() => sortedVisibleSelectedDevices.value.map((instance) => ({
  value: instanceKey(instance.deviceId, instance.deployId),
  label: instance.userName || instance.userId || instance.deployId,
  group: instance.deviceName || instance.deviceId,
  meta: `${instance.deployId} · ${instance.version || '版本未知'}`,
  status: instance.status,
})))
const realtimeResourceWatermark = computed(() => {
  const values = [data.value?.updatedAt || '']
  for (const application of data.value?.items || []) {
    if (application.resources.updatedAt) values.push(application.resources.updatedAt)
    for (const instance of application.devices) {
      if (instance.resources.updatedAt) values.push(instance.resources.updatedAt)
      if (instance.collectedAt) values.push(instance.collectedAt)
    }
  }
  return values.sort().at(-1) || ''
})
let lastRealtimeResourceWatermark = ''

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
watch(filtered, (items) => {
  if (items.length && !items.some((item) => item.id === selectedAppId.value)) selectedAppId.value = items[0].id
})
watch(selectedAppId, () => {
  if (selectedInstanceKey.value === 'all') loadHistory()
  else selectedInstanceKey.value = 'all'
})
watch(selectedInstanceKey, loadHistory)
watch(historyHours, () => {
  if (historyMode.value === 'preset') loadCurrentView()
})
watch([globalRealtime, realtimeResourceWatermark], ([enabled, watermark]) => {
  if (!enabled) {
    lastRealtimeResourceWatermark = ''
    return
  }
  if (!watermark || watermark === lastRealtimeResourceWatermark) return
  lastRealtimeResourceWatermark = watermark
  loadCurrentView()
}, { flush: 'post' })
const selectedInstanceBusy = computed(() => instanceAction.value !== '')
const autostartLabel = computed(() => selectedInstance.value?.autostart === true
  ? '开机自启动已开启'
  : selectedInstance.value?.autostart === false ? '开机自启动已关闭' : '设置开机自启动')

async function waitForApplicationOperation(id: string) {
  const deadline = Date.now() + 45_000
  while (Date.now() < deadline) {
    await new Promise((resolve) => window.setTimeout(resolve, 1000))
    const operation = await api<ApplicationOperation>(`/api/v1/application-operations/${encodeURIComponent(id)}`)
    if (operation.status === 'succeeded') return operation
    if (operation.status === 'failed') throw new Error(operation.error || '远端设备拒绝了应用操作')
  }
  throw new Error('操作已发送到远端设备，但状态回读超时；请稍后刷新确认')
}

async function controlSelectedInstance(action: 'start' | 'stop' | 'set_autostart', autostart?: boolean) {
  const instance = selectedInstance.value
  const application = selectedApp.value
  if (!instance || !application || selectedInstanceBusy.value) return
  if (instance.controllable === false) {
    emit('toast', '该系统或监控实例不允许在 WatchCat 中修改运行状态')
    return
  }
  const actionLabel = action === 'start' ? '启动' : action === 'stop' ? '停止' : `${autostart ? '开启' : '关闭'}开机自启动`
  const confirmed = await appConfirm({
    title: `${actionLabel}应用实例`,
    message: `${application.title || application.id}\n设备：${instance.deviceName || instance.deviceId}\n用户：${instance.userName || instance.userId || '未知'}\n实例：${instance.deployId}`,
    confirmText: actionLabel,
    danger: action === 'stop',
  })
  if (!confirmed) return
  instanceAction.value = action
  try {
    const result = await api<ApplicationOperation>(
      `/api/v1/applications/${encodeURIComponent(application.id)}/instances/${encodeURIComponent(instance.deployId)}/actions`,
      { method: 'POST', body: JSON.stringify({ deviceId: instance.deviceId, action, autostart }) },
    )
    if (result.id && (result.status === 'pending' || result.status === 'running')) await waitForApplicationOperation(result.id)
    emit('toast', `${actionLabel}成功`)
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    instanceAction.value = ''
  }
}
async function loadHistory() {
  if (!selectedAppId.value) return
  const request = ++historyRequest
  if (selectedInstance.value && selectedInstance.value.status !== 'running') {
    history.value = undefined
    historyLoading.value = false
    historyError.value = '该实例当前未运行，已隐藏设备级历史，避免误认为数据由该用户实例产生。'
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
    const deploy = selectedInstance.value?.deployId ? `&deployId=${encodeURIComponent(selectedInstance.value.deployId)}` : ''
    const userID = !selectedInstance.value && userFilter.value !== 'all' ? `&userId=${encodeURIComponent(userFilter.value)}` : ''
    const result = await api<HistoryPayload>(`/api/v1/applications/${encodeURIComponent(selectedAppId.value)}/metrics?${range}${device}${deploy}${userID}`)
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
    const device = deviceFilter.value !== 'all' ? `&deviceId=${encodeURIComponent(deviceFilter.value)}` : ''
    const userID = userFilter.value !== 'all' ? `&userId=${encodeURIComponent(userFilter.value)}` : ''
    for (const metric of ['cpu', 'memory', 'network', 'disk'] as ComparisonMetric[]) {
      next[metric] = await api<ComparisonPayload>(`/api/v1/applications/metrics/compare?metric=${metric}&scope=instance&${range}${device}${userID}`)
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
function toggleApplicationSort(metric: typeof sortMetric.value) {
  if (sortMetric.value === metric) sortDescending.value = !sortDescending.value
  else {
    sortMetric.value = metric
    sortDescending.value = metric !== 'name'
  }
}
function sortIndicator(metric: typeof sortMetric.value) {
  return sortMetric.value === metric ? (sortDescending.value ? ' ↓' : ' ↑') : ''
}
function selectPreset(hours: number) {
  historyMode.value = 'preset'
  showCustomRange.value = false
  customRangeError.value = ''
  if (historyHours.value === hours) loadCurrentView()
  else historyHours.value = hours
}
function applyCustomRange() {
  const from = parseBeijingDateTimeInput(customFrom.value)
  const to = parseBeijingDateTimeInput(customTo.value)
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
    at: dateTime(item.collectedAt),
    label: timeOfDay(item.collectedAt),
  }))
}
const cpuSeries = computed<ChartSeries[]>(() => [{ name: 'CPU', color: metricColors.cpu, points: chartPoints(history.value?.series.cpuPercent) }])
const memorySeries = computed<ChartSeries[]>(() => [{ name: '内存', color: metricColors.memory, points: chartPoints(history.value?.series.memoryUsage, 1024 * 1024) }])
const networkSeries = computed<ChartSeries[]>(() => [
  { name: '接收', color: metricColors.receive, points: chartPoints(history.value?.series.networkReceiveRate, 1024) },
  { name: '发送', color: metricColors.transmit, points: chartPoints(history.value?.series.networkTransmitRate, 1024) },
])
const blockSeries = computed<ChartSeries[]>(() => [
  { name: '读取', color: metricColors.read, points: chartPoints(history.value?.series.blockReadRate, 1024) },
  { name: '写入', color: metricColors.write, points: chartPoints(history.value?.series.blockWriteRate, 1024) },
])
const customHistoryRangeLabel = computed(() => historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
  ? `${dateTime(appliedCustomFrom.value)} 至 ${dateTime(appliedCustomTo.value)}`
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
      const device = application.devices.find((candidate) =>
        candidate.deviceId === item.deviceId && (!item.deployId || candidate.deployId === item.deployId))
      const scopeName = device?.userName || item.userId
      return {
        ...item,
        key: comparisonItemKey(item),
        appTitle: application.title || application.id,
        scopeName,
        title: `${application.title || application.id} / ${scopeName ? `${scopeName} · ` : ''}${device?.deviceName || item.deviceId}`,
        deviceName: device?.deviceName || item.deviceId || '未知设备',
      }
    })
    .sort((a, b) => (sortDescending.value ? b.value - a.value : a.value - b.value) || a.title.localeCompare(b.title))
}
function comparisonItemKey(item: ComparisonItem) {
  return `${item.appId}\0${item.deviceId || ''}\0${item.deployId || ''}\0${item.userId || ''}`
}
const comparisonPalette = ['#2563eb', '#7c3aed', '#059669', '#d97706', '#dc2626', '#0891b2', '#4f46e5', '#65a30d', '#db2777', '#475569']
function comparisonColor(item: ComparisonItem) {
  const key = comparisonItemKey(item)
  let hash = 0
  for (const character of key) hash = ((hash << 5) - hash + character.charCodeAt(0)) | 0
  return comparisonPalette[Math.abs(hash) % comparisonPalette.length]
}
const comparisonInstanceOptions = computed<SmartOption[]>(() => {
  const options = new Map<string, SmartOption>()
  for (const metric of ['cpu', 'memory', 'network', 'disk'] as ComparisonMetric[]) {
    for (const item of comparisonItems(metric)) {
      if (!options.has(item.key)) options.set(item.key, {
        value: item.key,
        label: `${item.appTitle}${item.scopeName ? ` · ${item.scopeName}` : ''}`,
        group: item.deviceName,
        meta: item.deployId || item.appId,
      })
    }
  }
  return [...options.values()].sort((a, b) => `${a.group} ${a.label}`.localeCompare(`${b.group} ${b.label}`))
})
function selectComparisonSeries(key: string) {
  comparisonInstanceKey.value = comparisonInstanceKey.value === key ? 'all' : key
}
watch(comparisonInstanceOptions, (options) => {
  if (comparisonInstanceKey.value !== 'all' && !options.some((item) => item.value === comparisonInstanceKey.value)) comparisonInstanceKey.value = 'all'
})
const comparisonGroups = computed<Array<{ metric: ComparisonMetric; title: string; unit: string; loaded: boolean; series: ChartSeries[] }>>(() => {
  const definitions: Array<{ metric: ComparisonMetric; title: string; unit: string; scale: number }> = [
    { metric: 'cpu', title: '所有应用 CPU', unit: '%', scale: 1 },
    { metric: 'memory', title: '所有应用内存', unit: ' MiB', scale: 1024 ** 2 },
    { metric: 'network', title: '所有应用网络流量', unit: ' MiB', scale: 1024 ** 2 },
    { metric: 'disk', title: '所有应用磁盘 I/O', unit: ' MiB', scale: 1024 ** 2 },
  ]
  return definitions.map((definition) => ({
    ...definition,
    loaded: Boolean(comparisons.value[definition.metric]),
    series: comparisonItems(definition.metric)
      .filter((item) => comparisonInstanceKey.value === 'all' || item.key === comparisonInstanceKey.value)
      .map((item) => ({
      id: item.key,
      name: item.title,
      color: comparisonColor(item),
      points: chartPoints(item.points, definition.scale),
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
        <select v-model="deviceFilter" aria-label="应用设备"><option value="all">全部设备</option><option v-for="device in availableDevices" :key="device.id" :value="device.id">{{ device.name }}</option></select>
        <select v-model="statusFilter" aria-label="应用状态"><option value="all">全部状态</option><option value="healthy">运行正常</option><option value="degraded">已暂停</option><option value="critical">异常</option></select>
        <select v-model="userFilter" aria-label="实例用户"><option value="all">全部用户</option><option v-for="user in availableUsers" :key="user.id" :value="user.id">{{ user.name || user.id }}</option></select>
        <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索应用名称"></label>
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
          <label><span>开始时间（北京时间）</span><input v-model="customFrom" type="datetime-local"></label>
          <label><span>结束时间（北京时间）</span><input v-model="customTo" type="datetime-local"></label>
          <button class="primary-button" @click="applyCustomRange">应用时间范围</button>
          <button class="secondary-button" @click="showCustomRange = false">取消</button>
          <small v-if="customRangeError">{{ customRangeError }}</small>
        </div>
      </div>
    </section>

    <div v-if="viewMode === 'detail'" class="app-resource-layout">
      <aside class="card app-resource-list-card">
        <div class="section-title compact"><div><h2>{{ userFilter === 'all' ? '部署实例所属应用' : '该用户的部署实例' }}</h2><span class="muted">{{ filtered.length }} 个结果</span></div></div>
        <p v-if="userFilter !== 'all'" class="instance-scope-note">已按 LazyCat 可见应用权限显示管理员授权的应用；没有用户独立实例时仍会保留，并明确标注暂无指标。</p>
        <div v-if="filtered.length" class="app-resource-list" role="table" aria-label="应用资源列表">
          <div class="app-resource-list-head" role="row">
            <button role="columnheader" @click="toggleApplicationSort('name')">应用{{ sortIndicator('name') }}</button>
            <button role="columnheader" @click="toggleApplicationSort('cpu')">CPU{{ sortIndicator('cpu') }}</button>
            <button role="columnheader" @click="toggleApplicationSort('memory')">内存{{ sortIndicator('memory') }}</button>
            <button role="columnheader" @click="toggleApplicationSort('network')">网络{{ sortIndicator('network') }}</button>
            <button role="columnheader" @click="toggleApplicationSort('disk')">I/O{{ sortIndicator('disk') }}</button>
          </div>
          <button v-for="item in appPagination.pagedItems.value" :key="item.id" :class="['app-resource-item', { active: selectedAppId === item.id }]" role="row" @click="selectedAppId = item.id">
            <span role="cell" class="app-resource-name"><i :class="scopedRuntimeState(item).tone" /><span><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small><em v-if="userFilter !== 'all' && !visibleDevices(item).length">暂无独立实例数据</em></span></span>
            <b role="cell">{{ formatNumber(scopedApplicationResources(item).cpuPercent) }}%</b>
            <small role="cell">{{ bytes(scopedApplicationResources(item).memoryUsage) }}</small>
            <small role="cell">{{ bytes(scopedApplicationResources(item).networkReceive + scopedApplicationResources(item).networkTransmit) }}</small>
            <small role="cell">{{ bytes(scopedApplicationResources(item).blockRead + scopedApplicationResources(item).blockWrite) }}</small>
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
              <StatusPill v-if="selectedInstance" :status="runtimeStatusTone(selectedInstance.status)" :label="runtimeStatusLabel(selectedInstance.status)" />
              <StatusPill v-else :status="scopedRuntimeState(selectedApp).tone" :label="scopedRuntimeState(selectedApp).label" />
            </div>
          </div>
          <div v-if="selectedInstance" class="instance-control-bar">
            <div class="instance-control-identity">
              <span>{{ selectedInstance.deviceName || selectedInstance.deviceId }}</span>
              <b>{{ selectedInstance.userName || selectedInstance.userId || '未知用户' }}</b>
              <code>{{ selectedInstance.deployId }}</code>
            </div>
            <div v-if="selectedInstance.controllable !== false" class="instance-control-actions">
              <button
                v-if="selectedInstance.status === 'paused' || selectedInstance.status === 'error'"
                class="primary-button"
                :disabled="selectedInstanceBusy"
                @click="controlSelectedInstance('start')"
              >{{ instanceAction === 'start' ? '启动中…' : '启动实例' }}</button>
              <button
                v-else
                class="danger-button"
                :disabled="selectedInstanceBusy || selectedInstance.status !== 'running'"
                @click="controlSelectedInstance('stop')"
              >{{ instanceAction === 'stop' ? '停止中…' : '停止实例' }}</button>
              <button
                :class="selectedInstance.autostart === true ? 'autostart-button active' : 'autostart-button'"
                :disabled="selectedInstanceBusy"
                @click="controlSelectedInstance('set_autostart', selectedInstance.autostart !== true)"
              ><i aria-hidden="true" />{{ instanceAction === 'set_autostart' ? '保存中…' : autostartLabel }}</button>
            </div>
            <span v-else class="protected-instance-note">受保护实例，仅监控</span>
          </div>
          <div class="app-resource-kpis">
            <div><span>当前 CPU</span><strong>{{ formatNumber(activeResources?.cpuPercent ?? 0) }}%</strong><small>{{ activeResources?.containers ?? 0 }} 个容器</small></div>
            <div><span>当前内存</span><strong>{{ bytes(activeResources?.memoryUsage ?? 0) }}</strong><small>{{ percent(activeResources?.memoryUsage ?? 0, activeResources?.memoryLimit ?? 0) }} 配额</small></div>
            <div><span>区间流量总和</span><strong>{{ bytes(history?.summary?.networkTotalBytes ?? 0) }}</strong><small>接收 {{ bytes(history?.summary?.networkReceiveRateBytes ?? 0) }} · 发送 {{ bytes(history?.summary?.networkTransmitRateBytes ?? 0) }}</small></div>
            <div><span>区间磁盘 IO</span><strong>{{ bytes(history?.summary?.blockTotalBytes ?? 0) }}</strong><small>读取 {{ bytes(history?.summary?.blockReadRateBytes ?? 0) }} · 写入 {{ bytes(history?.summary?.blockWriteRateBytes ?? 0) }}</small></div>
          </div>
          <p v-if="selectedInstance || userFilter !== 'all'" class="operation-evidence">
            访问范围来自 LazyCat AppAccessPolicy；资源指标按设备、用户和部署实例归属。
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

      </main>
    </div>

    <section v-else class="app-comparison-view">
      <div class="comparison-instance-filter">
        <label>应用实例</label>
        <SmartSelect v-model="comparisonInstanceKey" :options="comparisonInstanceOptions" :all-label="`全部实例（${comparisonInstanceOptions.length}）`" control-label="对比应用实例" searchable />
      </div>
      <p v-if="userFilter !== 'all'" class="operation-evidence">对比数据已按所选用户的部署实例筛选；实例级历史从 v1.3.7 开始积累。</p>
      <p v-if="comparisonLoading" class="operation-evidence">正在依次计算 CPU、内存、网络流量和磁盘 I/O…</p>
      <div v-if="comparisonError" class="inline-empty">对比数据加载失败：{{ comparisonError }} <button class="row-link" @click="loadComparison">重试</button></div>
      <template v-else>
        <div class="all-app-metric-grid">
          <section v-for="group in comparisonGroups" :key="group.metric" class="all-app-metric-panel">
            <div class="section-title compact"><div><h3>{{ group.title }}</h3></div><span class="pill unknown">{{ group.series.length }} 个应用实例</span></div>
            <div v-if="!group.loaded" class="metric-panel-loading">正在计算…</div>
            <LineChart v-else :series="group.series" :min="0" :unit="group.unit" :height="360" :show-legend="false" selectable @series-select="selectComparisonSeries" />
          </section>
        </div>
        <div v-if="!comparisonLoading && comparisonGroups.every((group) => !group.series.some((series) => series.points.length))" class="inline-empty">当前时间范围内没有可对比的应用指标。</div>
      </template>
    </section>
  </PageState>
</template>
