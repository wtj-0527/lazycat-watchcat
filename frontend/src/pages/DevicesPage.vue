<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling, useRovingTabs } from '@/composables'
import type { Capability, Device, Metric, Overview } from '@/types'
import { ago, connectivityState, dateTime, deviceState, formatMetricValue, metricValueAny, statusRank, storageRiskStatus } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import BarChart, { type BarItem } from '@/components/BarChart.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import DeviceTable from '@/components/DeviceTable.vue'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

type DetailTab = 'overview' | 'system' | 'storage' | 'apps' | 'network' | 'events'
const detailTabs: Array<[DetailTab, string]> = [
  ['overview', '概览'], ['system', '系统'], ['storage', '存储与硬件'],
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
const deviceCapabilities = ref<Capability[]>([])
interface SavedView { id: string; name: string; query: { query?: string; status?: string; connectivity?: string; capability?: string; group?: string } }
interface Payload extends Overview { savedViews: SavedView[] }
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const overview = await api<Overview & { savedViews?: SavedView[] }>('/api/v1/overview')
  return { ...overview, devices: overview.devices || [], alerts: overview.alerts || [], savedViews: overview.savedViews || [] }
})

const filteredDevices = computed(() => (data.value?.devices || [])
  .filter((device) => {
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

async function showDevice(id: string) {
  detailDeviceId.value = id
  detailLoading.value = true
  detailError.value = ''
  selectedTab.value = 'overview'
  try {
    selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(id)}`)
    const [events, operations] = await Promise.all([
      api<{ items: typeof deviceEvents.value }>(`/api/v1/devices/${encodeURIComponent(id)}/events`),
      api<{ capabilities: Array<Capability & { deviceId?: string }> }>('/api/v1/operations'),
    ])
    deviceEvents.value = events.items || []
    deviceCapabilities.value = (operations.capabilities || []).filter((item) => !item.deviceId || item.deviceId === id)
    await loadTrend(id)
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    detailLoading.value = false
  }
}
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
    const metricNames = ['system.cpu.usage', 'system.memory.usage', 'filesystem.root.usage']
    const histories = await Promise.all(metricNames.map(async (name) => {
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
function selectTrendPreset(hours: number) {
  trendMode.value = 'preset'
  trendHours.value = hours
  void loadTrend()
}
function showTrendCustomRange() {
  trendMode.value = 'custom'
  if (!trendCustomTo.value) {
    const now = new Date()
    trendCustomTo.value = toLocalInput(now)
    trendCustomFrom.value = toLocalInput(new Date(now.getTime() - 24 * 60 * 60 * 1000))
  }
}
function toLocalInput(date: Date) {
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}
function applyTrendCustomRange() {
  const from = new Date(trendCustomFrom.value)
  const to = new Date(trendCustomTo.value)
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
  const name = window.prompt('保存视图名称')
  if (!name) return
  await api('/api/v1/saved-views', {
    method: 'POST',
    body: JSON.stringify({ name, query: { query: query.value, status: statusFilter.value, connectivity: connectivityFilter.value, capability: capabilityFilter.value, group: groupFilter.value } }),
  })
  await refresh()
}
async function editMetadata() {
  if (!selected.value) return
  const group = window.prompt('设备组', selected.value.group || '') ?? (selected.value.group || '')
  const location = window.prompt('位置', selected.value.location || '') ?? (selected.value.location || '')
  await api(`/api/v1/devices/${encodeURIComponent(selected.value.id)}/metadata`, {
    method: 'PUT', body: JSON.stringify({ group, location, labels: selected.value.labels || {} }),
  })
  selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(selected.value.id)}`)
}
async function deleteDevice() {
  if (!selected.value || selected.value.local || deletingDevice.value) return
  const device = selected.value
  if (!window.confirm(`确定双向彻底移除设备“${device.name}”吗？本机将永久删除该设备的指标、告警、运行状态和凭据；对方下次通信时会自动清除上游配置。之后必须使用新邀请重新接入。`)) return
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
const trendSeries = computed<ChartSeries[]>(() => [
  { name: '处理器', color: '#2563eb', points: trend.value['system.cpu.usage'] || [] },
  { name: '内存', color: '#c05600', points: trend.value['system.memory.usage'] || [] },
  { name: '存储', color: '#7c3aed', points: trend.value['filesystem.root.usage'] || [] },
].filter((item) => item.points.length).map((item) => ({
  ...item,
  points: item.points.map((point) => ({
    value: point.value,
    at: dateTime(point.collectedAt),
    label: new Date(point.collectedAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
  })),
})))

function closeDetail() {
  selected.value = undefined
  detailDeviceId.value = ''
  detailError.value = ''
}
function metrics(device: Device): Metric[] {
  return Object.values(device.latest || {}).flat().sort((a, b) => a.name.localeCompare(b.name))
}
function categoryMetrics(device: Device, category: DetailTab): Metric[] {
  const prefixes: Record<DetailTab, string[]> = {
    overview: [], system: ['system.'], storage: ['filesystem.', 'disk.', 'btrfs.'],
    apps: ['container.'], network: ['network.', 'container.network.'], events: [],
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
function chartItems(items: Metric[], transform: (value: number) => number = (value) => value): BarItem[] {
  return latestByResource(items).map((point) => ({
    label: metricResource(point),
    value: Number(transform(point.value).toFixed(Math.abs(transform(point.value)) >= 10 ? 1 : 2)),
    color: storageRiskStatus(point) === 'critical' ? '#c51d23' : storageRiskStatus(point) === 'warning' ? '#c05600' : '#2563eb',
    hint: `${point.name} · ${formatMetricValue(point.value, point.unit)} · ${dateTime(point.collectedAt)}`,
  })).sort((a, b) => b.value - a.value).slice(0, 16)
}
interface MetricChartGroup { title: string; unit: string; items: BarItem[] }
const metricChartGroups = computed<MetricChartGroup[]>(() => {
  if (!selected.value) return []
  const items = categoryMetrics(selected.value, selectedTab.value)
  const groups: MetricChartGroup[] = []
  const add = (title: string, unit: string, values: Metric[], transform?: (value: number) => number) => {
    const bars = chartItems(values, transform)
    if (bars.length) groups.push({ title, unit, items: bars })
  }
  if (selectedTab.value === 'system') {
    add('资源使用率', '%', items.filter((point) => point.unit === '%' && point.name.includes('usage')))
    add('系统负载', '', items.filter((point) => point.name.startsWith('system.load.')))
    add('温度传感器', ' ℃', items.filter((point) => point.unit === 'celsius'))
    add('运行与交换空间', ' GiB', items.filter((point) => point.unit === 'bytes'), (value) => value / 1024 ** 3)
  } else if (selectedTab.value === 'storage') {
    add('存储卷使用率', '%', items.filter((point) => point.unit === '%' && point.name.includes('usage')))
    add('磁盘温度', ' ℃', items.filter((point) => point.name === 'disk.temperature'))
    add('容量与可用空间', ' GiB', items.filter((point) => point.unit === 'bytes' && (point.name.includes('size') || point.name.includes('capacity') || point.name.includes('available') || point.name.includes('free'))), (value) => value / 1024 ** 3)
    add('SMART 与 Btrfs 错误', '', items.filter((point) => point.unit === 'count' || point.unit === 'bitmask'))
  } else if (selectedTab.value === 'apps') {
    add('容器 CPU', '%', items.filter((point) => point.unit === '%' && point.name.includes('cpu')))
    add('容器内存', ' MiB', items.filter((point) => point.unit === 'bytes' && point.name.includes('memory')), (value) => value / 1024 ** 2)
    add('容器状态', '', items.filter((point) => point.name === 'container.running' || point.unit === 'bool' || point.unit === 'count'))
    add('容器 I/O 与流量', ' MiB', items.filter((point) => point.unit === 'bytes' && !point.name.includes('memory')), (value) => value / 1024 ** 2)
  } else if (selectedTab.value === 'network') {
    add('网络流量', ' MiB', items.filter((point) => point.unit === 'bytes'), (value) => value / 1024 ** 2)
    add('丢包与错误', '', items.filter((point) => point.unit === 'count'))
  }
  return groups
})
const eventTypeItems = computed<BarItem[]>(() => {
  const counts = new Map<string, number>()
  for (const item of deviceEvents.value) counts.set(item.type || '其他', (counts.get(item.type || '其他') || 0) + 1)
  return [...counts.entries()].map(([label, value]) => ({ label, value, color: '#2563eb' })).sort((a, b) => b.value - a.value)
})
const eventTimelineItems = computed<BarItem[]>(() => {
  const counts = new Map<string, number>()
  for (const item of deviceEvents.value) {
    const date = new Date(item.createdAt)
    const key = Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
    counts.set(key, (counts.get(key) || 0) + 1)
  }
  return [...counts.entries()].map(([label, value]) => ({ label, value, color: '#7c3aed' })).slice(-14)
})
const riskMetrics = computed(() => selected.value ? metrics(selected.value).filter((point) => {
  if (storageRiskStatus(point)) return true
  return (point.name === 'system.cpu.usage' || point.name === 'system.memory.usage') && point.value >= 85
}) : [])
const capabilityCount = computed(() => selected.value
  ? ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.'].filter((prefix) => Object.keys(selected.value?.latest || {}).some((name) => name.startsWith(prefix))).length
  : 0)
</script>

<template>
  <div v-if="selected || detailLoading || detailError">
    <button class="back-button" @click="closeDetail"><AppIcon name="arrow-left" :size="16" /> 返回设备清单</button>
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
                <div><h2>资源趋势</h2></div>
                <div class="range-tabs" aria-label="设备资源趋势时间范围">
                  <button v-for="option in [{ h: 1, l: '1 小时' }, { h: 6, l: '6 小时' }, { h: 24, l: '24 小时' }, { h: 168, l: '7 天' }]" :key="option.h" :class="{ active: trendMode === 'preset' && trendHours === option.h }" @click="selectTrendPreset(option.h)">{{ option.l }}</button>
                  <button :class="{ active: trendMode === 'custom' }" @click="showTrendCustomRange">自定义</button>
                </div>
              </div>
              <div v-if="trendMode === 'custom'" class="device-trend-custom-range">
                <label>开始<input v-model="trendCustomFrom" type="datetime-local"></label>
                <label>结束<input v-model="trendCustomTo" type="datetime-local"></label>
                <button class="secondary-button" @click="applyTrendCustomRange">应用</button>
              </div>
              <p v-if="trendError" class="operation-evidence warning">{{ trendError }}</p>
              <div v-if="trendLoading" class="inline-empty">正在读取资源历史…</div>
              <LineChart v-else :series="trendSeries" :min="0" :max="100" unit="%" :height="230" />
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
              <div v-for="item in deviceCapabilities" :key="item.capability" class="capability-line"><i :class="{ warning: item.status === 'restricted', unknown: item.status === 'unsupported' || item.status === 'error' }" /><b>{{ item.capability }}</b><span>{{ item.status }}</span></div>
              <a href="#settings">查看权限原因与修复步骤 →</a>
            </aside>
          </div>
        </div>

        <section v-else-if="selectedTab === 'events'" class="card">
          <div class="section-title"><div><h2>设备事件</h2></div></div>
          <div v-if="deviceEvents.length" class="device-metric-chart-grid">
            <section class="metric-chart-panel"><h3>事件类型构成</h3><BarChart :items="eventTypeItems" /></section>
            <section class="metric-chart-panel"><h3>最近事件趋势</h3><BarChart :items="eventTimelineItems" /></section>
          </div>
          <div v-else class="inline-empty">暂无设备事件。</div>
        </section>

        <section v-else class="card">
          <div class="section-title"><div><h2>{{ detailTabs.find(([key]) => key === selectedTab)?.[1] }}指标</h2></div></div>
          <div v-if="metricChartGroups.length" class="device-metric-chart-grid">
            <section v-for="group in metricChartGroups" :key="group.title" class="metric-chart-panel">
              <h3>{{ group.title }}</h3>
              <BarChart :items="group.items" :unit="group.unit" />
            </section>
          </div>
          <div v-else class="inline-empty">当前没有此分类的采集指标。</div>
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
    <section class="card">
      <div class="section-title"><div><h2>设备清单</h2></div></div>
      <DeviceTable v-if="filteredDevices.length" :items="filteredDevices" clickable @select="showDevice" />
      <div v-else class="inline-empty">没有符合当前筛选条件的设备。</div>
    </section>
  </PageState>
</template>
