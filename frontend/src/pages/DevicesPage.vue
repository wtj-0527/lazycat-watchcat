<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling, useRovingTabs } from '@/composables'
import type { Capability, Device, HostProcess, Metric, Overview } from '@/types'
import { ago, bytes, connectivityState, dateTime, deviceState, formatMetricValue, formatNumber, metricValueAny, monthDay, parseBeijingDateTimeInput, statusRank, storageRiskStatus, timeOfDay, toBeijingDateTimeInput } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import AppPagination from '@/components/AppPagination.vue'
import BarChart, { type BarItem } from '@/components/BarChart.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import ResourceRankingBoard, { type ResourceRankingItem } from '@/components/ResourceRankingBoard.vue'
import DeviceTable from '@/components/DeviceTable.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'
import { appConfirm, appPrompt } from '@/dialog'
import { globalRealtime } from '@/realtime'
import { metricColors } from '@/metricColors'
import { globalDeviceId } from '@/deviceScope'

type DetailTab = 'overview' | 'system' | 'processes' | 'storage' | 'apps' | 'network' | 'events'
const detailTabs: Array<[DetailTab, string]> = [
  ['overview', '概览'], ['system', '系统'], ['processes', '进程'], ['storage', '存储与硬件'],
  ['apps', '应用与容器'], ['network', '网络'], ['events', '事件'],
]

const selected = ref<Device>()
const detailDeviceId = ref('')
const { selected: selectedTab, select: selectDetailTab, move: moveDetailTab } = useRovingTabs(detailTabs, 'overview', 'device-tab-')
const detailLoading = ref(false)
const detailError = ref('')
const deletingDevice = ref(false)
const query = ref(sessionStorage.getItem('watchcatSearch') || '')
const statusFilter = ref('all')
const connectivityFilter = ref('all')
const capabilityFilter = ref('all')
const groupFilter = ref('all')
const selectedView = ref('all')
const trend = ref<Record<string, Metric[]>>({})
const trendMode = ref<'preset' | 'custom'>('preset')
const trendHours = ref(24)
const trendCustomFrom = ref('')
const trendCustomTo = ref('')
const trendAppliedFrom = ref('')
const trendAppliedTo = ref('')
const trendLoading = ref(false)
const trendError = ref('')
const deviceEvents = ref<Array<{ id: string; type: string; title: string; detail: Record<string, unknown>; createdAt: string }>>([])
const eventFilter = ref<'all' | 'alert' | 'audit'>('all')
const deviceCapabilities = ref<Capability[]>([])
const applicationTitles = ref<Record<string, string>>({})
interface DisplayProcess extends HostProcess {
  appId?: string
  appTitle?: string
  deployId?: string
  userId?: string
  containerName?: string
}
interface ProcessApplicationFilter {
  appId: string
  appTitle: string
  deployId?: string
  userId?: string
}
interface DeviceIOSourcePayload {
  applications: Array<ProcessApplicationFilter & { processes?: DisplayProcess[] }>
}
const processItems = ref<DisplayProcess[]>([])
const processTotal = ref(0)
const processPage = ref(1)
const processPageSize = ref(20)
const processQuery = ref('')
const processSort = ref('cpu')
const processOrder = ref<'asc' | 'desc'>('desc')
const processLoading = ref(false)
const processError = ref('')
const selectedProcess = ref<HostProcess>()
const processHistory = ref<HostProcess[]>([])
const processHistoryLoading = ref(false)
const processApplicationFilter = ref<ProcessApplicationFilter>()
const processDeepLink = (() => {
  const params = new URLSearchParams(location.hash.split('?')[1] || '')
  return {
    deviceId: params.get('deviceId') || '',
    tab: params.get('tab') || '',
    appId: params.get('appId') || '',
    appTitle: params.get('appTitle') || '',
    deployId: params.get('deployId') || '',
    userId: params.get('userId') || '',
  }
})()
let processDeepLinkApplied = false
const realtimeMetricNames = [
  'system.cpu.usage', 'system.memory.usage', 'system.swap.usage', 'system.load.1m',
  'filesystem.root.usage', 'btrfs.usage', 'disk.temperature',
  'disk.io.read.bytes_total', 'disk.io.write.bytes_total',
  'disk.io.read.operations_total', 'disk.io.write.operations_total',
  'network.interface.receive.bytes_total', 'network.interface.transmit.bytes_total',
]
interface SavedView { id: string; name: string; query: { query?: string; status?: string; connectivity?: string; capability?: string; group?: string } }
interface Payload extends Overview { savedViews: SavedView[] }
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const overview = await api<Overview & { savedViews?: SavedView[] }>('/api/v1/overview')
  return { ...overview, devices: overview.devices || [], alerts: overview.alerts || [], savedViews: overview.savedViews || [] }
})

const filteredDevices = computed(() => (data.value?.devices || [])
  .filter((device) => {
    if (globalDeviceId.value !== 'all' && device.id !== globalDeviceId.value) return false
    const text = `${device.name} ${device.hostname} ${device.osVersion}`.toLowerCase()
    const matchesQuery = text.includes(query.value.trim().toLowerCase())
    const state = deviceState(device)
    const matchesStatus = statusFilter.value === 'all' || (statusFilter.value === 'attention' ? state === 'critical' || state === 'warning' : state === statusFilter.value)
    const connection = connectivityState(device)
    const matchesConnection = connectivityFilter.value === 'all' || (connectivityFilter.value === 'unavailable' ? connection === 'offline' || connection === 'stale' : connection === connectivityFilter.value)
    const capability = Object.keys(device.latest || {}).some((name) => name.startsWith('disk.') || name.startsWith('btrfs.')) ? 'full' : 'limited'
    const matchesCapability = capabilityFilter.value === 'all' || capabilityFilter.value === capability
    const matchesGroup = groupFilter.value === 'all' || (device.group || '') === groupFilter.value
    return matchesQuery && matchesStatus && matchesConnection && matchesCapability && matchesGroup
  })
  .sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))))
const devicePagination = usePagination(filteredDevices, 20)
watch([query, statusFilter, connectivityFilter, capabilityFilter, groupFilter, globalDeviceId], devicePagination.resetPage)

async function showDevice(id: string, initialTab: DetailTab = 'overview') {
  detailDeviceId.value = id
  detailLoading.value = true
  detailError.value = ''
  selectedTab.value = initialTab === 'processes' ? 'overview' : initialTab
  if (initialTab !== 'processes') processApplicationFilter.value = undefined
  try {
    selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(id)}`)
    const [events, operations, applications] = await Promise.all([
      api<{ items: typeof deviceEvents.value }>(`/api/v1/devices/${encodeURIComponent(id)}/events`),
      api<{ capabilities: Array<Capability & { deviceId?: string }> }>('/api/v1/operations'),
      api<{ items: Array<{ id: string; title: string }> }>('/api/v1/applications').catch(() => ({ items: [] })),
    ])
    deviceEvents.value = events.items || []
    deviceCapabilities.value = (operations.capabilities || []).filter((item) => !item.deviceId || item.deviceId === id)
    applicationTitles.value = Object.fromEntries((applications.items || []).map((item) => [item.id, item.title || item.id]))
    await loadTrend(id)
    if (initialTab === 'processes') selectedTab.value = 'processes'
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    detailLoading.value = false
  }
}
async function loadDeviceEvents(id = selected.value?.id || detailDeviceId.value) {
  if (!id) return
  try {
    const result = await api<{ items: typeof deviceEvents.value }>(`/api/v1/devices/${encodeURIComponent(id)}/events`)
    deviceEvents.value = result.items || []
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  }
}
async function loadProcesses() {
  const id = selected.value?.id || detailDeviceId.value
  if (!id) return
  processLoading.value = true
  processError.value = ''
  try {
    if (processApplicationFilter.value) {
      const filter = processApplicationFilter.value
      const result = await api<DeviceIOSourcePayload>(`/api/v1/devices/${encodeURIComponent(id)}/io-sources?limit=50&page=1`)
      const matches = (result.applications || []).filter((application) =>
        application.appId === filter.appId
        && (!filter.deployId || application.deployId === filter.deployId)
        && (!filter.userId || application.userId === filter.userId))
      const unique = new Map<string, DisplayProcess>()
      for (const process of matches.flatMap((application) => application.processes || [])) {
        unique.set(`${process.pid}\u0000${process.startTime}`, process)
      }
      let items = [...unique.values()]
      const query = processQuery.value.trim().toLowerCase()
      if (query) {
        items = items.filter((item) =>
          `${item.pid} ${item.name} ${item.user} ${item.command || ''} ${item.containerName || ''}`.toLowerCase().includes(query))
      }
      const direction = processOrder.value === 'asc' ? 1 : -1
      const value = (item: DisplayProcess): string | number => ({
        pid: item.pid, name: item.name, user: item.user, state: item.state,
        cpu: item.cpuPercent, memory: item.memoryRssBytes, read: item.readRate,
        write: item.writeRate, threads: item.threads, uptime: item.uptimeSeconds,
      } as Record<string, string | number>)[processSort.value] ?? item.cpuPercent
      items.sort((left, right) => {
        const a = value(left)
        const b = value(right)
        if (typeof a === 'number' && typeof b === 'number') return (a - b) * direction
        return String(a).localeCompare(String(b)) * direction
      })
      processTotal.value = items.length
      const offset = (processPage.value - 1) * processPageSize.value
      processItems.value = items.slice(offset, offset + processPageSize.value)
      return
    }
    const params = new URLSearchParams({
      page: String(processPage.value), limit: String(processPageSize.value),
      sort: processSort.value, order: processOrder.value,
    })
    if (processQuery.value.trim()) params.set('q', processQuery.value.trim())
    const result = await api<{ items: HostProcess[]; total: number }>(`/api/v1/devices/${encodeURIComponent(id)}/processes?${params}`)
    processItems.value = result.items || []
    processTotal.value = result.total || 0
  } catch (reason) {
    processError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    processLoading.value = false
  }
}
function clearProcessApplicationFilter() {
  processApplicationFilter.value = undefined
  processPage.value = 1
  void loadProcesses()
}
async function selectProcess(item: HostProcess) {
  selectedProcess.value = item
  processHistoryLoading.value = true
  try {
    const result = await api<{ items: HostProcess[] }>(`/api/v1/devices/${encodeURIComponent(selected.value!.id)}/processes/${item.pid}/metrics?startTime=${encodeURIComponent(item.startTime)}&${trendRangeQuery()}`)
    processHistory.value = result.items || []
  } catch (reason) {
    processError.value = reason instanceof Error ? reason.message : String(reason)
    processHistory.value = []
  } finally {
    processHistoryLoading.value = false
  }
}
function applyProcessFilters() {
  processPage.value = 1
  void loadProcesses()
}
function sortProcesses(column: string) {
  if (processSort.value === column) processOrder.value = processOrder.value === 'desc' ? 'asc' : 'desc'
  else {
    processSort.value = column
    processOrder.value = column === 'name' || column === 'user' || column === 'state' || column === 'pid' ? 'asc' : 'desc'
  }
  processPage.value = 1
  void loadProcesses()
}
function processSortIndicator(column: string) {
  return processSort.value === column ? (processOrder.value === 'desc' ? ' ↓' : ' ↑') : ''
}
function changeProcessPage(page: number) {
  processPage.value = page
  void loadProcesses()
}
function changeProcessPageSize(size: number) {
  processPageSize.value = size
  processPage.value = 1
  void loadProcesses()
}
const processPageCount = computed(() => Math.max(1, Math.ceil(processTotal.value / processPageSize.value)))
const processRangeStart = computed(() => processTotal.value ? (processPage.value - 1) * processPageSize.value + 1 : 0)
const processRangeEnd = computed(() => Math.min(processTotal.value, processPage.value * processPageSize.value))
const processKpis = computed(() => ({
  total: processTotal.value,
  running: processItems.value.filter((item) => item.state === 'R').length,
  cpu: [...processItems.value].sort((a, b) => b.cpuPercent - a.cpuPercent)[0],
  memory: [...processItems.value].sort((a, b) => b.memoryRssBytes - a.memoryRssBytes)[0],
}))
const processCpuSeries = computed<ChartSeries[]>(() => selectedProcess.value ? [{
  name: selectedProcess.value.name, color: metricColors.cpu,
  points: processHistory.value.map((item) => ({ value: item.cpuPercent, at: dateTime(item.collectedAt), label: timeOfDay(item.collectedAt) })),
}] : [])
const processMemorySeries = computed<ChartSeries[]>(() => selectedProcess.value ? [{
  name: selectedProcess.value.name, color: metricColors.memory,
  points: processHistory.value.map((item) => ({ value: item.memoryRssBytes / 1024 ** 2, at: dateTime(item.collectedAt), label: timeOfDay(item.collectedAt) })),
}] : [])
const processIoSeries = computed<ChartSeries[]>(() => selectedProcess.value ? [
  { name: '读取', color: metricColors.read, points: processHistory.value.map((item) => ({ value: item.readRate / 1024, at: dateTime(item.collectedAt), label: timeOfDay(item.collectedAt) })) },
  { name: '写入', color: metricColors.write, points: processHistory.value.map((item) => ({ value: item.writeRate / 1024, at: dateTime(item.collectedAt), label: timeOfDay(item.collectedAt) })) },
] : [])
function trendRangeQuery() {
  return trendMode.value === 'custom' && trendAppliedFrom.value && trendAppliedTo.value
    ? `from=${encodeURIComponent(trendAppliedFrom.value)}&to=${encodeURIComponent(trendAppliedTo.value)}`
    : `hours=${trendHours.value}`
}
async function loadTrend(id = selected.value?.id || detailDeviceId.value) {
  if (!id) return
  trendLoading.value = true
  trendError.value = ''
  try {
    const histories = await Promise.all(realtimeMetricNames.map(async (name) => {
      const result = await api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(id)}/metrics?name=${encodeURIComponent(name)}&${trendRangeQuery()}`)
      return [name, result.items || []] as const
    }))
    trend.value = Object.fromEntries(histories)
  } catch (reason) {
    trendError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    trendLoading.value = false
  }
}
function metricIdentity(point: Metric) {
  const labels = Object.entries(point.labels || {}).sort(([left], [right]) => left.localeCompare(right))
  return `${point.collectedAt}\u0000${JSON.stringify(labels)}`
}
function appendLatestTrend(device: Device) {
  const now = Date.now()
  const from = trendMode.value === 'custom' && trendAppliedFrom.value
    ? new Date(trendAppliedFrom.value).getTime()
    : now - trendHours.value * 60 * 60 * 1000
  const to = trendMode.value === 'custom' && trendAppliedTo.value
    ? new Date(trendAppliedTo.value).getTime()
    : now
  const next = { ...trend.value }
  for (const name of realtimeMetricNames) {
    const existing = next[name] || []
    const seen = new Set(existing.map(metricIdentity))
    const additions = (device.latest?.[name] || []).filter((point) => {
      const at = new Date(point.collectedAt).getTime()
      return at >= from && at <= to && !seen.has(metricIdentity(point))
    })
    if (!additions.length) continue
    next[name] = [...existing, ...additions]
      .filter((point) => {
        const at = new Date(point.collectedAt).getTime()
        return at >= from && at <= to
      })
      .sort((left, right) => new Date(left.collectedAt).getTime() - new Date(right.collectedAt).getTime())
  }
  trend.value = next
}
function selectTrendPreset(hours: number) {
  trendMode.value = 'preset'
  trendHours.value = hours
  void loadTrend()
  if (selectedProcess.value) void selectProcess(selectedProcess.value)
}
function showTrendCustomRange() {
  trendMode.value = 'custom'
  if (!trendCustomTo.value) {
    const now = new Date()
    trendCustomTo.value = toBeijingDateTimeInput(now)
    trendCustomFrom.value = toBeijingDateTimeInput(new Date(now.getTime() - 24 * 60 * 60 * 1000))
  }
}
function applyTrendCustomRange() {
  const from = parseBeijingDateTimeInput(trendCustomFrom.value)
  const to = parseBeijingDateTimeInput(trendCustomTo.value)
  if (!trendCustomFrom.value || !trendCustomTo.value || Number.isNaN(from.getTime()) || Number.isNaN(to.getTime()) || from >= to) {
    trendError.value = '请选择有效的开始和结束时间'
    return
  }
  if (to.getTime() - from.getTime() > 30 * 24 * 60 * 60 * 1000) {
    trendError.value = '单次查询范围不能超过 30 天'
    return
  }
  trendAppliedFrom.value = from.toISOString()
  trendAppliedTo.value = to.toISOString()
  trendError.value = ''
  void loadTrend()
  if (selectedProcess.value) void selectProcess(selectedProcess.value)
}
const groups = computed(() => [...new Set((data.value?.devices || []).map((item) => item.group).filter(Boolean))] as string[])
function applyView(view: SavedView['query']) {
  query.value = view.query || ''
  statusFilter.value = view.status || 'all'
  connectivityFilter.value = view.connectivity || 'all'
  capabilityFilter.value = view.capability || 'all'
  groupFilter.value = view.group || 'all'
}
function applySelectedView() {
  const presets: Record<string, SavedView['query']> = {
    all: {},
    attention: { status: 'attention' },
    limited: { capability: 'limited' },
    unavailable: { connectivity: 'unavailable' },
  }
  if (presets[selectedView.value]) {
    applyView(presets[selectedView.value])
    return
  }
  const saved = data.value?.savedViews.find((view) => `saved:${view.id}` === selectedView.value)
  if (saved) applyView(saved.query)
}
function markCustomView() {
  selectedView.value = 'custom'
}
async function saveView() {
  const name = await appPrompt({ title: '保存当前视图', message: '为当前筛选条件设置一个容易识别的名称。', inputPlaceholder: '视图名称', confirmText: '保存视图' })
  if (!name) return
  await api('/api/v1/saved-views', {
    method: 'POST',
    body: JSON.stringify({ name, query: { query: query.value, status: statusFilter.value, connectivity: connectivityFilter.value, capability: capabilityFilter.value, group: groupFilter.value } }),
  })
  await refresh()
}
async function editMetadata() {
  if (!selected.value) return
  const group = await appPrompt({ title: '修改设备组', inputValue: selected.value.group || '', inputPlaceholder: '设备组', confirmText: '下一步' })
  if (group === null) return
  const location = await appPrompt({ title: '修改设备位置', inputValue: selected.value.location || '', inputPlaceholder: '位置', confirmText: '保存资料' })
  if (location === null) return
  await api(`/api/v1/devices/${encodeURIComponent(selected.value.id)}/metadata`, {
    method: 'PUT', body: JSON.stringify({ group, location, labels: selected.value.labels || {} }),
  })
  selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(selected.value.id)}`)
}
async function deleteDevice() {
  if (!selected.value || selected.value.local || deletingDevice.value) return
  const device = selected.value
  if (!await appConfirm({ title: '彻底移除设备', message: `确定双向彻底移除设备“${device.name}”吗？本机将永久删除该设备的指标、告警、运行状态和凭据；对方下次通信时会自动清除上游配置。之后必须使用新邀请重新接入。`, confirmText: '彻底移除', danger: true })) return
  deletingDevice.value = true
  detailError.value = ''
  try {
    await api(`/api/v1/devices/${encodeURIComponent(device.id)}`, { method: 'DELETE' })
    closeDetail()
    await refresh()
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    deletingDevice.value = false
  }
}
function newestPoint(name: string, predicate?: (point: Metric) => boolean): Metric | undefined {
  return (selected.value?.latest?.[name] || [])
    .filter((point) => !predicate || predicate(point))
    .sort((a, b) => new Date(b.collectedAt).getTime() - new Date(a.collectedAt).getTime())[0]
}
function pointValue(name: string, fallback = '未知') {
  const point = newestPoint(name)
  return point ? formatMetricValue(point.value, point.unit) : fallback
}
function latestTotal(name: string) {
  return (selected.value?.latest?.[name] || []).reduce((sum, point) => sum + point.value, 0)
}
function historySeries(name: string, label: string, color: string, transform: (value: number) => number = (value) => value): ChartSeries {
  return {
    name: label,
    color,
    points: (trend.value[name] || []).map((point) => ({
      value: transform(point.value),
      at: dateTime(point.collectedAt),
      label: timeOfDay(point.collectedAt),
    })),
  }
}
const systemUsageSeries = computed<ChartSeries[]>(() => [
  historySeries('system.cpu.usage', 'CPU', metricColors.cpu),
  historySeries('system.memory.usage', '内存', metricColors.memory),
  historySeries('system.swap.usage', 'Swap', metricColors.swap),
].filter((item) => item.points.length))
const systemLoadSeries = computed<ChartSeries[]>(() => [
  historySeries('system.load.1m', '1 分钟负载', metricColors.load),
].filter((item) => item.points.length))
const temperatureSummary = computed(() => {
  const points = selected.value?.latest?.['system.temperature'] || []
  const groups = [
    { label: 'CPU Package', match: (sensor: string) => sensor.includes('package'), color: '#c51d23' },
    { label: 'CPU 最高核心', match: (sensor: string) => sensor.includes('core'), color: '#c05600' },
    { label: 'NVMe', match: (sensor: string) => sensor.includes('nvme'), color: '#7c3aed' },
    { label: '其他传感器', match: (sensor: string) => !sensor.includes('package') && !sensor.includes('core') && !sensor.includes('nvme'), color: '#2563eb' },
  ]
  return groups.map((group) => {
    const matches = points.filter((point) => group.match((point.labels?.sensor || '').toLowerCase()))
    const hottest = matches.sort((a, b) => b.value - a.value)[0]
    return { ...group, value: hottest?.value, sensor: hottest?.labels?.sensor || '无数据' }
  })
})
const hottestSensors = computed<BarItem[]>(() => (selected.value?.latest?.['system.temperature'] || [])
  .map((point) => ({
    label: point.labels?.sensor || '未知传感器',
    value: Number(point.value.toFixed(1)),
    color: point.value >= 80 ? '#c51d23' : point.value >= 70 ? '#c05600' : '#2563eb',
    hint: `${point.name} · ${dateTime(point.collectedAt)}`,
  }))
  .sort((a, b) => b.value - a.value)
  .slice(0, 10))

interface StorageDiskView {
  device: string
  model: string
  serial: string
  media: string
  transport: string
  capacity: number
  temperature?: number
  powerHours?: number
  status: 'healthy' | 'warning' | 'critical' | 'unknown'
  smartObserved: boolean
  smartEvidence: string[]
}
const storageDisks = computed<StorageDiskView[]>(() => {
  const points = selected.value ? categoryMetrics(selected.value, 'storage') : []
  const normalizedDevice = (point: Metric) => String(point.labels?.device || '').replace('/dev/', '')
  const canonical = new Map<string, Metric>()
  for (const point of points.filter((item) => item.name.startsWith('disk.'))) {
    const device = normalizedDevice(point)
    if (!device || device.startsWith('dm-')) continue
    const key = `${device}\u0000${point.name}`
    const current = canonical.get(key)
    const pointTime = new Date(point.collectedAt).getTime()
    const currentTime = current ? new Date(current.collectedAt).getTime() : 0
    const preferredSource = point.labels?.source === 'lazycat-docker-helper'
      && current?.labels?.source !== 'lazycat-docker-helper'
    if (!current || pointTime > currentTime || (pointTime === currentTime && preferredSource)) canonical.set(key, point)
  }
  const devices = new Map<string, StorageDiskView>()
  for (const point of canonical.values()) {
    const device = normalizedDevice(point)
    const current = devices.get(device) || {
      device,
      model: point.labels?.model || '未知型号',
      serial: point.labels?.serial || '未知序列号',
      media: point.labels?.media || (device.startsWith('nvme') ? 'ssd' : '未知'),
      transport: point.labels?.transport || '未知',
      capacity: 0,
      status: 'healthy' as const,
      smartObserved: false,
      smartEvidence: [],
    }
    current.model = point.labels?.model || current.model
    current.serial = point.labels?.serial || current.serial
    current.media = point.labels?.media || current.media
    current.transport = point.labels?.transport || current.transport
    if (point.name === 'disk.capacity') current.capacity = point.value
    if (point.name === 'disk.temperature') { current.temperature = point.value; current.smartObserved = true }
    if (point.name === 'disk.power_on_hours') { current.powerHours = point.value; current.smartObserved = true }
    if (point.name === 'disk.nvme.critical_warning' && point.value > 0) {
      current.smartObserved = true
      current.status = 'critical'
      current.smartEvidence.push(`NVMe 严重警告 0x${Math.round(point.value).toString(16).toUpperCase()}`)
    }
    if (point.name === 'disk.nvme.media_errors' && point.value > 0) {
      current.smartObserved = true
      current.status = 'critical'
      current.smartEvidence.push(`NVMe 介质错误 ${formatNumber(point.value, 0)}`)
    }
    if (point.name === 'disk.ata.reallocated_sectors' && point.value > 0) {
      current.smartObserved = true
      if (current.status !== 'critical') current.status = 'warning'
      current.smartEvidence.push(`重映射扇区 ${formatNumber(point.value, 0)}`)
    }
    if (point.name === 'disk.ata.pending_sectors' && point.value > 0) {
      current.smartObserved = true
      current.status = 'critical'
      current.smartEvidence.push(`待处理扇区 ${formatNumber(point.value, 0)}`)
    }
    if (point.name === 'disk.ata.offline_uncorrectable' && point.value > 0) {
      current.smartObserved = true
      current.status = 'critical'
      current.smartEvidence.push(`离线不可校正 ${formatNumber(point.value, 0)}`)
    }
    if (point.name === 'disk.ata.reported_uncorrectable' && point.value > 0) {
      current.smartObserved = true
      current.status = 'critical'
      current.smartEvidence.push(`已报告不可校正 ${formatNumber(point.value, 0)}`)
    }
    devices.set(device, current)
  }
  for (const disk of devices.values()) {
    const warningTemperature = disk.media === 'hdd' ? 55 : 70
    const criticalTemperature = disk.media === 'hdd' ? 65 : 80
    if ((disk.temperature || 0) >= criticalTemperature) disk.status = 'critical'
    else if ((disk.temperature || 0) >= warningTemperature && disk.status === 'healthy') disk.status = 'warning'
    else if (!disk.smartObserved) disk.status = 'unknown'
  }
  const rank = { critical: 0, warning: 1, unknown: 2, healthy: 3 }
  return [...devices.values()].sort((a, b) => rank[a.status] - rank[b.status] || b.capacity - a.capacity)
})
const storageVolumes = computed(() => {
  if (!selected.value) return []
  return latestByResource(categoryMetrics(selected.value, 'storage')
    .filter((point) => point.name === 'btrfs.usage' || point.name === 'filesystem.root.usage'))
    .map((point) => ({
      mount: point.labels?.mount || point.labels?.path || point.labels?.device || '根文件系统',
      value: point.value,
      collectedAt: point.collectedAt,
      source: point.name,
    }))
    .sort((a, b) => b.value - a.value)
})
const storageVolumeItems = computed<BarItem[]>(() => storageVolumes.value.map((item) => ({
  label: item.mount,
  value: Number(item.value.toFixed(1)),
  color: item.value >= 90 ? '#c51d23' : item.value >= 75 ? '#c05600' : '#2563eb',
  hint: `${item.source} · ${dateTime(item.collectedAt)}`,
})))
const primaryVolumeMount = computed(() => storageVolumes.value.find((item) => item.source === 'btrfs.usage')?.mount || '')
const storageTrendSeries = computed<ChartSeries[]>(() => {
  const points = (trend.value['btrfs.usage'] || []).filter((point) =>
    !primaryVolumeMount.value || (point.labels?.mount || point.labels?.path) === primaryVolumeMount.value)
  return points.length ? [{
    name: primaryVolumeMount.value || '最高使用率卷',
    color: metricColors.storage,
    points: points.map((point) => ({
      value: point.value,
      at: dateTime(point.collectedAt),
      label: timeOfDay(point.collectedAt),
    })),
  }] : []
})
const btrfsEvidence = computed(() => selected.value ? latestByResource(categoryMetrics(selected.value, 'storage').filter((point) =>
  point.name.startsWith('btrfs.') && point.value > 0 && (
    point.name.includes('errors') || point.name === 'btrfs.device_missing'
  ))) : [])

function counterRateSeries(points: Metric[], label: string, color: string, divisor = 1024 ** 2): ChartSeries {
  const totals = new Map<string, number>()
  for (const point of points) totals.set(point.collectedAt, (totals.get(point.collectedAt) || 0) + point.value)
  const ordered = [...totals.entries()].sort((a, b) => new Date(a[0]).getTime() - new Date(b[0]).getTime())
  const result: ChartSeries['points'] = []
  for (let index = 1; index < ordered.length; index += 1) {
    const seconds = (new Date(ordered[index][0]).getTime() - new Date(ordered[index - 1][0]).getTime()) / 1000
    if (seconds <= 0) continue
    const rate = Math.max(0, ordered[index][1] - ordered[index - 1][1]) / seconds / divisor
    result.push({
      value: rate,
      at: dateTime(ordered[index][0]),
      label: timeOfDay(ordered[index][0]),
    })
  }
  return { name: label, color, points: result }
}
const diskRateSeries = computed<ChartSeries[]>(() => [
  counterRateSeries(trend.value['disk.io.read.bytes_total'] || [], '读取', metricColors.read),
  counterRateSeries(trend.value['disk.io.write.bytes_total'] || [], '写入', metricColors.write),
].filter((item) => item.points.length))
const diskOperationSeries = computed<ChartSeries[]>(() => [
  counterRateSeries(trend.value['disk.io.read.operations_total'] || [], '读 IOPS', metricColors.read, 1),
  counterRateSeries(trend.value['disk.io.write.operations_total'] || [], '写 IOPS', metricColors.write, 1),
].filter((item) => item.points.length))
const networkRateSeries = computed<ChartSeries[]>(() => [
  counterRateSeries(trend.value['network.interface.receive.bytes_total'] || [], '下载', metricColors.receive),
  counterRateSeries(trend.value['network.interface.transmit.bytes_total'] || [], '上传', metricColors.transmit),
].filter((item) => item.points.length))
const currentNetworkRates = computed(() => Object.fromEntries(networkRateSeries.value.map((item) => [item.name, item.points.at(-1)?.value || 0])))
const networkAppItems = computed<BarItem[]>(() => {
  const apps = new Map<string, number>()
  for (const item of containerResources.value) apps.set(item.appTitle, (apps.get(item.appTitle) || 0) + item.network)
  return [...apps.entries()].map(([label, value]) => ({
    label,
    value: Number((value / 1024 ** 2).toFixed(1)),
    color: '#2563eb',
    hint: `容器累计收发 ${bytes(value)}`,
  })).sort((a, b) => b.value - a.value).slice(0, 12)
})
const networkErrors = computed(() => selected.value ? latestByResource(categoryMetrics(selected.value, 'network')
  .filter((point) => point.name.includes('errors_total') || point.name.includes('dropped_total'))
  .sort((a, b) => b.value - a.value)) : [])

const filteredDeviceEvents = computed(() => deviceEvents.value.filter((item) => eventFilter.value === 'all' || item.type === eventFilter.value))
const eventStats = computed(() => ({
  total: deviceEvents.value.length,
  alert: deviceEvents.value.filter((item) => item.type === 'alert').length,
  audit: deviceEvents.value.filter((item) => item.type === 'audit').length,
  severe: deviceEvents.value.filter((item) => String(item.detail?.severity || '').toLowerCase() === 'critical').length,
}))
function eventDetail(item: typeof deviceEvents.value[number]) {
  return Object.entries(item.detail || {}).map(([key, value]) => `${key}: ${String(value)}`).join(' · ') || '无附加证据'
}

function closeDetail() {
  selected.value = undefined
  detailDeviceId.value = ''
  detailError.value = ''
  selectedProcess.value = undefined
  processItems.value = []
  processHistory.value = []
}
function metrics(device: Device): Metric[] {
  return Object.values(device.latest || {}).flat().sort((a, b) => a.name.localeCompare(b.name))
}
function categoryMetrics(device: Device, category: DetailTab): Metric[] {
  const prefixes: Record<DetailTab, string[]> = {
    overview: [], system: ['system.'], storage: ['filesystem.', 'disk.', 'btrfs.'],
    apps: ['container.'], network: ['network.', 'container.network.'], processes: [], events: [],
  }
  if (!prefixes[category].length) return metrics(device)
  return metrics(device).filter((point) => prefixes[category].some((prefix) => point.name.startsWith(prefix)))
}
function metricResource(point: Metric): string {
  return point.labels?.app || point.labels?.container || point.labels?.device || point.labels?.mount
    || point.labels?.sensor || point.labels?.interface || point.name.split('.').slice(-2).join('.')
}
function latestByResource(items: Metric[]): Metric[] {
  const latest = new Map<string, Metric>()
  for (const point of items) {
    const key = `${point.name}:${metricResource(point)}`
    const current = latest.get(key)
    if (!current || new Date(point.collectedAt).getTime() > new Date(current.collectedAt).getTime()) latest.set(key, point)
  }
  return [...latest.values()]
}
interface ContainerResource {
  id: string
  app: string
  appTitle: string
  container: string
  containerName: string
  cpu: number
  memory: number
  memoryPercent: number
  network: number
  io: number
  running: boolean
  collectedAt: string
}
const containerResources = computed<ContainerResource[]>(() => {
  if (!selected.value) return []
  const resources = new Map<string, { labels: Record<string, string>; points: Map<string, Metric> }>()
  for (const point of categoryMetrics(selected.value, 'apps')) {
    const labels = point.labels || {}
    const app = labels.app || '未知应用'
    const container = labels.container || labels.name || labels.service
    if (!container) continue
    const key = `${app}\u0000${container}`
    const resource = resources.get(key) || { labels, points: new Map<string, Metric>() }
    const current = resource.points.get(point.name)
    if (!current || new Date(point.collectedAt).getTime() > new Date(current.collectedAt).getTime()) {
      resource.points.set(point.name, point)
      resource.labels = labels
    }
    resources.set(key, resource)
  }
  return [...resources.entries()].map(([id, resource]) => {
    const value = (name: string) => resource.points.get(name)?.value || 0
    const app = resource.labels.app || '未知应用'
    const memory = value('container.memory.usage')
    const limit = value('container.memory.limit')
    const collectedAt = [...resource.points.values()].sort((a, b) => new Date(b.collectedAt).getTime() - new Date(a.collectedAt).getTime())[0]?.collectedAt || ''
    return {
      id,
      app,
      appTitle: applicationTitles.value[app] || app,
      container: resource.labels.container || '',
      containerName: resource.labels.name || resource.labels.service || resource.labels.container || '未知容器',
      cpu: value('container.cpu.usage'),
      memory,
      memoryPercent: value('container.memory.usage_percent') || (limit > 0 ? memory / limit * 100 : 0),
      network: value('container.network.receive.bytes_total') + value('container.network.transmit.bytes_total'),
      io: value('container.block.read.bytes_total') + value('container.block.write.bytes_total'),
      running: value('container.running') >= 1,
      collectedAt,
    }
  })
})
const applicationRankingItems = computed<ResourceRankingItem[]>(() => containerResources.value.map((item) => ({
  id: item.id,
  label: item.appTitle,
  detail: `${item.containerName} · ${item.app}`,
  cpu: item.cpu,
  memory: item.memory,
  network: item.network,
  io: item.io,
  running: item.running,
})))
const eventTypeItems = computed<BarItem[]>(() => {
  const counts = new Map<string, number>()
  for (const item of deviceEvents.value) counts.set(item.type || '其他', (counts.get(item.type || '其他') || 0) + 1)
  return [...counts.entries()].map(([label, value]) => ({ label, value, color: '#2563eb' })).sort((a, b) => b.value - a.value)
})
const eventTimelineItems = computed<BarItem[]>(() => {
  const counts = new Map<string, number>()
  for (const item of deviceEvents.value) {
    const key = monthDay(item.createdAt) || '未知时间'
    counts.set(key, (counts.get(key) || 0) + 1)
  }
  return [...counts.entries()].map(([label, value]) => ({ label, value, color: '#7c3aed' })).slice(-14)
})
const capabilityPagination = usePagination(deviceCapabilities, 10)
const riskMetrics = computed(() => selected.value ? metrics(selected.value).filter((point) => {
  if (storageRiskStatus(point)) return true
  return (point.name === 'system.cpu.usage' || point.name === 'system.memory.usage') && point.value >= 85
}) : [])
const capabilityCount = computed(() => selected.value
  ? ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.'].filter((prefix) => Object.keys(selected.value?.latest || {}).some((name) => name.startsWith(prefix))).length
  : 0)
watch(() => data.value, (payload) => {
  if (!detailDeviceId.value || !payload) return
  const fresh = payload.devices.find((device) => device.id === detailDeviceId.value)
  if (!fresh) return
  selected.value = selected.value ? { ...selected.value, ...fresh } : fresh
  if (!globalRealtime.value) return
  appendLatestTrend(fresh)
  if (selectedTab.value === 'processes') {
    void loadProcesses()
    if (selectedProcess.value) void selectProcess(selectedProcess.value)
  } else if (selectedTab.value === 'events') {
    void loadDeviceEvents(fresh.id)
  }
}, { flush: 'post' })
watch(() => data.value?.devices, async (devices) => {
  if (processDeepLinkApplied || processDeepLink.tab !== 'processes' || !processDeepLink.deviceId || !processDeepLink.appId) return
  if (!(devices || []).some((device) => device.id === processDeepLink.deviceId)) return
  processDeepLinkApplied = true
  processApplicationFilter.value = {
    appId: processDeepLink.appId,
    appTitle: processDeepLink.appTitle || processDeepLink.appId,
    deployId: processDeepLink.deployId || undefined,
    userId: processDeepLink.userId || undefined,
  }
  await showDevice(processDeepLink.deviceId, 'processes')
}, { immediate: true })
watch(globalRealtime, (enabled) => {
  if (!enabled || !selected.value) return
  appendLatestTrend(selected.value)
  if (selectedTab.value === 'processes') {
    void loadProcesses()
    if (selectedProcess.value) void selectProcess(selectedProcess.value)
  } else if (selectedTab.value === 'events') {
    void loadDeviceEvents(selected.value.id)
  }
})
watch(selectedTab, (tab) => {
  if (tab === 'processes') void loadProcesses()
  if (tab === 'events') void loadDeviceEvents()
})
</script>

<template>
  <div v-if="selected || detailLoading || detailError">
    <button class="back-button" @click="closeDetail"><AppIcon name="arrow-left" :size="16" /> 返回设备</button>
    <PageState :loading="detailLoading" :error="detailError" @retry="detailDeviceId && showDevice(detailDeviceId)">
      <template v-if="selected">
        <section class="device-hero">
          <div>
            <div class="device-title-line"><h2>{{ selected.name }}</h2></div>
          </div>
          <div class="button-row">
            <StatusPill :status="deviceState(selected)" /><StatusPill :status="connectivityState(selected)" />
            <span class="pill unknown">{{ ago(selected.lastSeenAt) }}</span>
            <button class="secondary-button" @click="editMetadata">编辑资料</button>
            <button v-if="!selected.local" class="danger-button" :disabled="deletingDevice" @click="deleteDevice">{{ deletingDevice ? '移除中…' : '双向彻底移除' }}</button>
          </div>
        </section>

        <div class="tab-bar" role="tablist" aria-label="设备详情分类">
          <button
            v-for="[key, label] in detailTabs"
            :id="`device-tab-${key}`"
            :key="key"
            :class="{ active: selectedTab === key }"
            role="tab"
            :aria-selected="selectedTab === key"
            aria-controls="device-panel"
            :tabindex="selectedTab === key ? 0 : -1"
            @click="selectDetailTab(key)"
            @keydown="moveDetailTab($event, key)"
          >{{ label }}</button>
        </div>

        <div id="device-panel" role="tabpanel" :aria-labelledby="`device-tab-${selectedTab}`">
        <div v-if="selectedTab === 'overview'">
          <div class="device-overview-grid">
            <section class="card identity-card">
              <div class="section-title"><div><h2>设备身份</h2></div></div>
              <dl class="identity-list">
                <div><dt>设备组</dt><dd>{{ selected.group || '未分组' }}</dd></div>
                <div><dt>位置</dt><dd>{{ selected.location || '未设置' }}</dd></div>
                <div><dt>系统</dt><dd>{{ selected.osVersion || '未知' }}</dd></div>
                <div><dt>地址</dt><dd>{{ selected.hostname || '未知' }}</dd></div>
                <div><dt>采集器</dt><dd>{{ selected.collectorVersion || '未知' }}</dd></div>
              </dl>
            </section>
            <section class="card realtime-card">
              <div class="section-title"><div><h2>实时资源</h2></div></div>
              <div class="realtime-metrics">
                <div><span>处理器</span><strong>{{ metricValueAny(selected, ['system.cpu.usage']) }}</strong><i><em :style="{ width: metricValueAny(selected, ['system.cpu.usage']) }" /></i></div>
                <div><span>内存</span><strong>{{ metricValueAny(selected, ['system.memory.usage']) }}</strong><i><em :style="{ width: metricValueAny(selected, ['system.memory.usage']) }" /></i></div>
                <div><span>负载</span><strong>{{ metricValueAny(selected, ['system.load.1m'], 2) }}</strong><i><em style="width:36%" /></i></div>
                <div><span>存储</span><strong>{{ metricValueAny(selected, ['filesystem.root.usage', 'btrfs.usage']) }}</strong><i><em :style="{ width: metricValueAny(selected, ['filesystem.root.usage', 'btrfs.usage']) }" /></i></div>
              </div>
            </section>
            <section class="card resource-trend-card">
              <div class="section-title device-trend-title">
                <div><h2>磁盘 I/O 趋势</h2></div>
                <div class="range-tabs" aria-label="设备磁盘 I/O 趋势时间范围">
                  <button v-for="option in [{ h: 1, l: '1 小时' }, { h: 6, l: '6 小时' }, { h: 24, l: '24 小时' }, { h: 168, l: '7 天' }]" :key="option.h" :class="{ active: trendMode === 'preset' && trendHours === option.h }" @click="selectTrendPreset(option.h)">{{ option.l }}</button>
                  <button :class="{ active: trendMode === 'custom' }" @click="showTrendCustomRange">自定义</button>
                </div>
              </div>
              <div v-if="trendMode === 'custom'" class="device-trend-custom-range">
                <label>开始（北京时间）<input v-model="trendCustomFrom" type="datetime-local"></label>
                <label>结束（北京时间）<input v-model="trendCustomTo" type="datetime-local"></label>
                <button class="secondary-button" @click="applyTrendCustomRange">应用</button>
              </div>
              <p v-if="trendError" class="operation-evidence warning">{{ trendError }}</p>
              <div v-if="trendLoading" class="inline-empty">正在读取磁盘 I/O 历史…</div>
              <template v-else>
                <div class="resource-throughput-grid">
                  <section>
                    <header>
                      <div><h3>磁盘吞吐</h3><small>累计读写差值换算</small></div>
                      <div class="resource-throughput-legend"><span v-for="item in diskRateSeries" :key="item.name"><i :style="{background:item.color}" />{{item.name}}</span></div>
                    </header>
                    <LineChart :series="diskRateSeries" :min="0" unit=" MiB/s" :height="190" :show-legend="false" />
                  </section>
                  <section>
                    <header>
                      <div><h3>磁盘 IOPS</h3><small>每秒读写操作次数</small></div>
                      <div class="resource-throughput-legend"><span v-for="item in diskOperationSeries" :key="item.name"><i :style="{background:item.color}" />{{item.name}}</span></div>
                    </header>
                    <LineChart :series="diskOperationSeries" :min="0" unit=" IOPS" :height="190" :show-legend="false" />
                  </section>
                </div>
              </template>
            </section>
            <section class="card active-risk-card">
              <div class="section-title"><div><h2>活动风险</h2></div><span class="pill critical">{{ riskMetrics.length }} 个严重</span></div>
              <div v-if="riskMetrics.length" class="risk-evidence-list" role="table" aria-label="活动风险">
                <div class="risk-evidence-head" role="row"><span aria-hidden="true" /><span role="columnheader">风险与证据</span><span role="columnheader">操作</span></div>
                <div v-for="point in riskMetrics.slice(0, 3)" :key="`${point.name}-${JSON.stringify(point.labels)}`" role="row">
                  <i aria-hidden="true" /><span role="cell" data-label="风险与证据"><b>{{ point.name }}</b><small>{{ formatMetricValue(point.value, point.unit) }} · {{ ago(point.collectedAt) }}</small></span>
                  <button class="secondary-button tiny" @click="selectDetailTab(point.name.startsWith('system.') ? 'system' : 'storage')">查看完整证据</button>
                </div>
              </div>
              <div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有活动风险</b><small>以最新真实指标为准。</small></div></div>
            </section>
            <aside class="card capability-summary-card">
              <div class="section-title"><div><h2>采集能力</h2></div><span class="pill healthy">{{ capabilityCount }} 可用</span></div>
              <div v-if="deviceCapabilities.length" class="capability-line capability-line-head"><span aria-hidden="true" /><b>能力</b><span>状态</span></div>
              <div v-for="item in capabilityPagination.pagedItems.value" :key="item.capability" class="capability-line"><i :class="{ warning: item.status === 'restricted', unknown: item.status === 'unsupported' || item.status === 'error' }" /><b>{{ item.capability }}</b><span>{{ item.status }}</span></div>
              <AppPagination v-model:page="capabilityPagination.page.value" v-model:page-size="capabilityPagination.pageSize.value" :total="capabilityPagination.total.value" :page-count="capabilityPagination.pageCount.value" :range-start="capabilityPagination.rangeStart.value" :range-end="capabilityPagination.rangeEnd.value" label="设备采集能力分页" />
              <a href="#settings">查看权限原因与修复步骤 →</a>
            </aside>
          </div>
        </div>

        <section v-else-if="selectedTab === 'system'" class="device-detail-insights">
          <div class="detail-kpi-grid">
            <article><span>CPU</span><strong>{{ pointValue('system.cpu.usage') }}</strong><small>当前使用率</small></article>
            <article><span>内存</span><strong>{{ pointValue('system.memory.usage') }}</strong><small>当前使用率</small></article>
            <article><span>Swap</span><strong>{{ pointValue('system.swap.usage') }}</strong><small>交换空间使用率</small></article>
            <article><span>1 分钟负载</span><strong>{{ pointValue('system.load.1m') }}</strong><small>最近一次采集</small></article>
          </div>
          <section class="detail-chart-card full">
            <div class="section-title compact device-trend-title">
              <div><h3>资源使用趋势</h3><span class="muted">CPU、内存与 Swap</span></div>
              <div class="range-tabs" aria-label="系统趋势时间范围">
                <button v-for="option in [{ h: 1, l: '1 小时' }, { h: 6, l: '6 小时' }, { h: 24, l: '24 小时' }, { h: 168, l: '7 天' }]" :key="option.h" :class="{ active: trendMode === 'preset' && trendHours === option.h }" @click="selectTrendPreset(option.h)">{{ option.l }}</button>
                <button :class="{ active: trendMode === 'custom' }" @click="showTrendCustomRange">自定义</button>
              </div>
            </div>
            <div v-if="trendMode === 'custom'" class="device-trend-custom-range">
              <label>开始（北京时间）<input v-model="trendCustomFrom" type="datetime-local"></label>
              <label>结束（北京时间）<input v-model="trendCustomTo" type="datetime-local"></label>
              <button class="secondary-button" @click="applyTrendCustomRange">应用</button>
            </div>
            <LineChart :series="systemUsageSeries" :min="0" :max="100" unit="%" :height="220" />
          </section>
          <div class="detail-two-column">
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>系统负载</h3><span class="muted">1 分钟负载历史</span></div></div><LineChart :series="systemLoadSeries" :min="0" :height="205" /></section>
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>最高温度传感器</h3><span class="muted">按当前温度排序</span></div></div><BarChart :items="hottestSensors" unit=" ℃" /></section>
          </div>
          <div class="temperature-summary-grid">
            <article v-for="item in temperatureSummary" :key="item.label"><i :style="{ background: item.color }" /><span>{{ item.label }}</span><strong>{{ item.value === undefined ? '未知' : `${formatNumber(item.value)} ℃` }}</strong><small>{{ item.sensor }}</small></article>
          </div>
        </section>

        <section v-else-if="selectedTab === 'storage'" class="device-detail-insights">
          <div class="physical-disk-grid">
            <article v-for="disk in storageDisks" :key="disk.device" class="physical-disk-card" :class="disk.status">
              <header><div><i /><span><b>/dev/{{ disk.device }}</b><small>{{ disk.media.toUpperCase() }} · {{ disk.transport }}</small></span></div><StatusPill :status="disk.status" /></header>
              <h3>{{ disk.model }}</h3><p>{{ disk.serial }}</p>
              <dl><div><dt>容量</dt><dd>{{ disk.capacity ? bytes(disk.capacity) : '未知' }}</dd></div><div><dt>温度</dt><dd>{{ disk.temperature === undefined ? '未知' : `${formatNumber(disk.temperature)} ℃` }}</dd></div><div><dt>通电</dt><dd>{{ disk.powerHours === undefined ? '未知' : `${formatNumber(disk.powerHours, 0)} 小时` }}</dd></div><div><dt>SMART 证据</dt><dd :title="disk.smartEvidence.join('；')">{{ disk.smartEvidence.length ? disk.smartEvidence.join(' · ') : disk.smartObserved ? '未发现异常' : '尚无 SMART 数据' }}</dd></div></dl>
            </article>
          </div>
          <div class="detail-two-column storage-detail-grid">
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>存储卷使用率</h3><span class="muted">按当前使用率排序</span></div></div><BarChart :items="storageVolumeItems" unit="%" /></section>
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>{{ primaryVolumeMount || '主要存储卷' }} · 容量趋势</h3><span class="muted">默认 24 小时，可沿用系统页的时间范围</span></div></div><LineChart :series="storageTrendSeries" :min="0" :max="100" unit="%" :height="235" /></section>
          </div>
          <section class="detail-evidence-card">
            <div class="section-title compact"><div><h3>Btrfs 与 SMART 证据</h3><span class="muted">只展示非零错误与缺失设备，不混入读写累计计数。</span></div></div>
            <div v-if="btrfsEvidence.length" class="detail-evidence-list">
              <div v-for="point in btrfsEvidence" :key="`${point.name}:${metricResource(point)}`"><span><b>{{ point.name }}</b><small>{{ metricResource(point) }} · {{ dateTime(point.collectedAt) }}</small></span><strong>{{ formatMetricValue(point.value, point.unit) }}</strong></div>
            </div>
            <div v-else class="healthy-empty horizontal"><span>✓</span><div><b>未发现 Btrfs 或 SMART 错误</b><small>以最近一次真实采集结果为准。</small></div></div>
          </section>
        </section>

        <section v-else-if="selectedTab === 'processes'" class="device-detail-insights process-insights">
          <div v-if="processApplicationFilter" class="process-application-scope">
            <span><small>当前应用</small><b>{{ processApplicationFilter.appTitle }}</b><code>{{ processApplicationFilter.deployId || processApplicationFilter.appId }}</code></span>
            <button class="secondary-button" @click="clearProcessApplicationFilter">查看全部宿主机进程</button>
          </div>
          <div class="detail-kpi-grid">
            <article><span>进程数</span><strong>{{ processKpis.total }}</strong><small>当前宿主机快照</small></article>
            <article><span>运行中</span><strong>{{ processKpis.running }}</strong><small>当前页状态为 R</small></article>
            <article><span>CPU 最高</span><strong>{{ processKpis.cpu ? `${formatNumber(processKpis.cpu.cpuPercent)}%` : '未知' }}</strong><small>{{ processKpis.cpu?.name || '尚无数据' }}</small></article>
            <article><span>内存最高</span><strong>{{ processKpis.memory ? bytes(processKpis.memory.memoryRssBytes) : '未知' }}</strong><small>{{ processKpis.memory?.name || '尚无数据' }}</small></article>
          </div>
          <div class="process-toolbar">
            <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="processQuery" placeholder="搜索 PID、名称、用户或命令" @keyup.enter="applyProcessFilters"></label>
            <button class="secondary-button" @click="applyProcessFilters">查询</button>
          </div>
          <div v-if="processError" class="inline-empty">进程读取失败：{{ processError }} <button class="row-link" @click="loadProcesses">重试</button></div>
          <div v-else-if="processLoading" class="inline-empty">正在读取宿主机进程…</div>
          <div v-else class="table-scroll process-table-wrap">
            <table class="process-table">
              <thead><tr>
                <th><button @click="sortProcesses('pid')">PID{{ processSortIndicator('pid') }}</button></th>
                <th><button @click="sortProcesses('name')">进程{{ processSortIndicator('name') }}</button></th>
                <th><button @click="sortProcesses('user')">用户{{ processSortIndicator('user') }}</button></th>
                <th><button @click="sortProcesses('state')">状态{{ processSortIndicator('state') }}</button></th>
                <th><button @click="sortProcesses('cpu')">CPU{{ processSortIndicator('cpu') }}</button></th>
                <th><button @click="sortProcesses('memory')">内存{{ processSortIndicator('memory') }}</button></th>
                <th><button @click="sortProcesses('read')">读取{{ processSortIndicator('read') }}</button></th>
                <th><button @click="sortProcesses('write')">写入{{ processSortIndicator('write') }}</button></th>
                <th><button @click="sortProcesses('threads')">线程{{ processSortIndicator('threads') }}</button></th>
                <th><button @click="sortProcesses('uptime')">运行时间{{ processSortIndicator('uptime') }}</button></th>
              </tr></thead>
              <tbody>
                <tr v-for="item in processItems" :key="`${item.pid}:${item.startTime}`" :class="{ selected: selectedProcess?.pid === item.pid && selectedProcess?.startTime === item.startTime }" @click="selectProcess(item)">
                  <td>{{ item.pid }}</td><td><b>{{ item.name }}</b><small :title="item.command">{{ item.containerName || item.command || item.cgroup || '无命令行' }}</small></td><td>{{ item.user || '未知' }}</td><td>{{ item.state }}</td>
                  <td>{{ formatNumber(item.cpuPercent) }}%</td><td>{{ bytes(item.memoryRssBytes) }}</td><td>{{ bytes(item.readRate) }}/s</td><td>{{ bytes(item.writeRate) }}/s</td><td>{{ item.threads }}</td><td>{{ formatNumber(item.uptimeSeconds / 3600, 1) }} 小时</td>
                </tr>
              </tbody>
            </table>
          </div>
          <AppPagination :page="processPage" :page-size="processPageSize" :total="processTotal" :page-count="processPageCount" :range-start="processRangeStart" :range-end="processRangeEnd" label="宿主机进程分页" @update:page="changeProcessPage" @update:page-size="changeProcessPageSize" />
          <section v-if="selectedProcess" class="process-history-section">
            <div class="section-title compact device-trend-title">
              <div><h3>{{ selectedProcess.name }} · PID {{ selectedProcess.pid }}</h3><span class="muted">{{ selectedProcess.user }} · {{ selectedProcess.command || selectedProcess.cgroup }}</span></div>
              <div class="range-tabs" aria-label="单进程历史时间范围">
                <button v-for="option in [{ h: 1, l: '1 小时' }, { h: 6, l: '6 小时' }, { h: 24, l: '24 小时' }, { h: 168, l: '7 天' }]" :key="option.h" :class="{ active: trendMode === 'preset' && trendHours === option.h }" @click="selectTrendPreset(option.h)">{{ option.l }}</button>
                <button :class="{ active: trendMode === 'custom' }" @click="showTrendCustomRange">自定义</button>
              </div>
            </div>
            <div v-if="trendMode === 'custom'" class="device-trend-custom-range">
              <label>开始（北京时间）<input v-model="trendCustomFrom" type="datetime-local"></label>
              <label>结束（北京时间）<input v-model="trendCustomTo" type="datetime-local"></label>
              <button class="secondary-button" @click="applyTrendCustomRange">应用</button>
            </div>
            <div v-if="processHistoryLoading" class="inline-empty">正在读取单进程历史…</div>
            <div v-else class="process-history-grid">
              <div><h4>CPU</h4><LineChart :series="processCpuSeries" :min="0" unit="%" :height="220" /></div>
              <div><h4>内存</h4><LineChart :series="processMemorySeries" :min="0" unit=" MiB" :height="220" /></div>
              <div><h4>磁盘 I/O 速率</h4><LineChart :series="processIoSeries" :min="0" unit=" KiB/s" :height="220" /></div>
            </div>
          </section>
        </section>

        <section v-else-if="selectedTab === 'network'" class="device-detail-insights">
          <div class="detail-kpi-grid">
            <article><span>当前下载</span><strong>{{ formatNumber(currentNetworkRates['下载'] || 0) }} MiB/s</strong><small>根据相邻采集计数计算</small></article>
            <article><span>当前上传</span><strong>{{ formatNumber(currentNetworkRates['上传'] || 0) }} MiB/s</strong><small>根据相邻采集计数计算</small></article>
            <article><span>累计接收</span><strong>{{ bytes(latestTotal('network.interface.receive.bytes_total')) }}</strong><small>全部物理接口</small></article>
            <article><span>累计发送</span><strong>{{ bytes(latestTotal('network.interface.transmit.bytes_total')) }}</strong><small>全部物理接口</small></article>
          </div>
          <section class="detail-chart-card full">
            <div class="section-title compact device-trend-title">
              <div><h3>上传与下载速率</h3><span class="muted">由累计字节差值换算，单位 MiB/s</span></div>
              <div class="range-tabs" aria-label="网络趋势时间范围">
                <button v-for="option in [{ h: 1, l: '1 小时' }, { h: 6, l: '6 小时' }, { h: 24, l: '24 小时' }, { h: 168, l: '7 天' }]" :key="option.h" :class="{ active: trendMode === 'preset' && trendHours === option.h }" @click="selectTrendPreset(option.h)">{{ option.l }}</button>
                <button :class="{ active: trendMode === 'custom' }" @click="showTrendCustomRange">自定义</button>
              </div>
            </div>
            <LineChart :series="networkRateSeries" :min="0" unit=" MiB/s" :height="235" />
          </section>
          <div class="detail-two-column">
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>应用累计流量排行</h3><span class="muted">容器累计接收与发送之和</span></div></div><BarChart :items="networkAppItems" unit=" MiB" /></section>
            <section class="detail-evidence-card"><div class="section-title compact"><div><h3>接口丢包与错误</h3><span class="muted">按接口和方向显示原始计数</span></div></div><div v-if="networkErrors.length" class="detail-evidence-list"><div v-for="point in networkErrors" :key="`${point.name}:${metricResource(point)}`"><span><b>{{ metricResource(point) }}</b><small>{{ point.name }}</small></span><strong>{{ formatNumber(point.value, 0) }}</strong></div></div><div v-else class="healthy-empty horizontal"><span>✓</span><div><b>没有接口丢包或错误</b><small>最近一次计数均为 0。</small></div></div></section>
          </div>
        </section>

        <section v-else-if="selectedTab === 'events'" class="device-detail-insights">
          <div class="detail-kpi-grid event-kpis">
            <article><span>全部事件</span><strong>{{ eventStats.total }}</strong><small>当前保留窗口</small></article>
            <article><span>告警变化</span><strong>{{ eventStats.alert }}</strong><small>触发、升级与恢复</small></article>
            <article><span>审计操作</span><strong>{{ eventStats.audit }}</strong><small>设备配置与操作</small></article>
            <article><span>严重事件</span><strong>{{ eventStats.severe }}</strong><small>severity = critical</small></article>
          </div>
          <div class="event-layout">
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>最近事件趋势</h3><span class="muted">按自然日聚合</span></div></div><BarChart :items="eventTimelineItems" /></section>
            <section class="detail-chart-card"><div class="section-title compact"><div><h3>事件类型构成</h3><span class="muted">告警状态与审计操作</span></div></div><BarChart :items="eventTypeItems" /></section>
          </div>
          <section class="event-timeline-card">
            <div class="section-title compact"><div><h3>事件时间线</h3><span class="muted">展开查看服务端保存的原始证据。</span></div><div class="event-filter-tabs"><button :class="{ active: eventFilter === 'all' }" @click="eventFilter = 'all'">全部</button><button :class="{ active: eventFilter === 'alert' }" @click="eventFilter = 'alert'">告警</button><button :class="{ active: eventFilter === 'audit' }" @click="eventFilter = 'audit'">审计</button></div></div>
            <div v-if="filteredDeviceEvents.length" class="device-event-timeline">
              <details v-for="item in filteredDeviceEvents" :key="item.id"><summary><i :class="item.type" /><span><b>{{ item.title }}</b><small>{{ item.type === 'alert' ? '告警状态' : '审计操作' }} · {{ dateTime(item.createdAt) }}</small></span><em>查看证据</em></summary><p>{{ eventDetail(item) }}</p></details>
            </div>
            <div v-else class="inline-empty">当前筛选条件下没有设备事件。</div>
          </section>
        </section>

        <section v-else-if="selectedTab === 'apps'" class="device-app-insights">
          <section v-if="containerResources.length" class="application-ranking-section">
            <ResourceRankingBoard :items="applicationRankingItems" />
          </section>
          <div v-else class="inline-empty">当前没有容器资源指标。</div>
        </section>

        </div>
      </template>
    </PageState>
  </div>

  <PageState v-else :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro">
      <div><h2>设备</h2></div>
    </div>
    <div class="filter-bar">
      <select v-model="selectedView" class="view-select" aria-label="选择视图" @change="applySelectedView">
        <option v-if="selectedView === 'custom'" value="custom" disabled>自定义筛选</option>
        <optgroup label="快捷视图">
          <option value="all">全部设备</option>
          <option value="attention">需要处置</option>
          <option value="limited">能力受限</option>
          <option value="unavailable">离线或陈旧</option>
        </optgroup>
        <optgroup v-if="data?.savedViews.length" label="我的视图">
          <option v-for="view in data.savedViews" :key="view.id" :value="`saved:${view.id}`">{{ view.name }}</option>
        </optgroup>
      </select>
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="按名称、位置或标签搜索" @input="markCustomView"></label>
      <select v-model="statusFilter" aria-label="健康状态" @change="markCustomView"><option value="all">健康状态</option><option value="attention">需要处置</option><option value="critical">严重</option><option value="warning">警告</option><option value="healthy">健康</option><option value="offline">离线</option></select>
      <select v-model="connectivityFilter" aria-label="连接状态" @change="markCustomView"><option value="all">连接状态</option><option value="online">在线</option><option value="stale">陈旧</option><option value="offline">离线</option></select>
      <select v-model="capabilityFilter" aria-label="采集能力" @change="markCustomView"><option value="all">采集能力</option><option value="full">完整</option><option value="limited">受限</option></select>
      <select v-model="groupFilter" aria-label="设备组" @change="markCustomView"><option value="all">设备组</option><option v-for="group in groups" :key="group" :value="group">{{ group }}</option></select>
      <button class="secondary-button save-view-button" @click="saveView">保存当前视图</button>
    </div>
    <section class="device-list-section">
      <DeviceTable v-if="filteredDevices.length" :items="devicePagination.pagedItems.value" clickable @select="showDevice" />
      <div v-else class="inline-empty">没有符合当前筛选条件的设备。</div>
      <AppPagination v-model:page="devicePagination.page.value" v-model:page-size="devicePagination.pageSize.value" :total="devicePagination.total.value" :page-count="devicePagination.pageCount.value" :range-start="devicePagination.rangeStart.value" :range-end="devicePagination.rangeEnd.value" label="设备列表分页" />
    </section>
  </PageState>
</template>
