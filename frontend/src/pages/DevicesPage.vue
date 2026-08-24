<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling, useRovingTabs } from '@/composables'
import type { Capability, Device, Metric, Overview } from '@/types'
import { ago, connectivityState, dateTime, deviceState, formatMetricValue, metricValueAny, statusRank, storageRiskStatus } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
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
const query = ref(sessionStorage.getItem('maoyanSearch') || '')
const statusFilter = ref('all')
const connectivityFilter = ref('all')
const capabilityFilter = ref('all')
const groupFilter = ref('all')
const trend = ref<Record<string, Metric[]>>({})
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
    const metricNames = ['system.cpu.usage', 'system.memory.usage', 'filesystem.root.usage']
    const [histories, events, operations] = await Promise.all([
      Promise.all(metricNames.map(async (name) => {
        const result = await api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(id)}/metrics?name=${encodeURIComponent(name)}&hours=24`)
        return [name, result.items || []] as const
      })),
      api<{ items: typeof deviceEvents.value }>(`/api/v1/devices/${encodeURIComponent(id)}/events`),
      api<{ capabilities: Array<Capability & { deviceId?: string }> }>('/api/v1/operations'),
    ])
    trend.value = Object.fromEntries(histories)
    deviceEvents.value = events.items || []
    deviceCapabilities.value = (operations.capabilities || []).filter((item) => !item.deviceId || item.deviceId === id)
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    detailLoading.value = false
  }
}
const groups = computed(() => [...new Set((data.value?.devices || []).map((item) => item.group).filter(Boolean))] as string[])
function applyView(view: SavedView['query']) {
  query.value = view.query || ''
  statusFilter.value = view.status || 'all'
  connectivityFilter.value = view.connectivity || 'all'
  capabilityFilter.value = view.capability || 'all'
  groupFilter.value = view.group || 'all'
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
            <p>健康与连接状态独立呈现；每项风险都能追溯到采集证据。</p>
          </div>
          <div class="button-row">
            <StatusPill :status="deviceState(selected)" /><StatusPill :status="connectivityState(selected)" />
            <span class="pill unknown">{{ ago(selected.lastSeenAt) }}</span>
            <button class="secondary-button" @click="editMetadata">编辑资料</button>
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
            <section class="card resource-trend-card">
              <div class="section-title"><div><h2>24 小时资源趋势</h2><span class="muted">处理器、内存和根文件系统均来自历史指标 API。</span></div></div>
              <LineChart :series="trendSeries" :min="0" :max="100" unit="%" :height="210" />
            </section>
          </div>
        </div>

        <section v-else-if="selectedTab === 'events'" class="card">
          <div class="section-title"><div><h2>设备事件</h2><span class="muted">告警与审计事件</span></div></div>
          <div v-if="deviceEvents.length" class="backup-list"><div v-for="item in deviceEvents" :key="item.id" class="backup-row"><div><b>{{ item.title }}</b><p>{{ dateTime(item.createdAt) }} · {{ item.type }}</p><code>{{ JSON.stringify(item.detail) }}</code></div></div></div>
          <div v-else class="inline-empty">暂无设备事件。</div>
        </section>

        <section v-else class="card">
          <div class="section-title">
            <div><h2>{{ detailTabs.find(([key]) => key === selectedTab)?.[1] }}指标</h2><span class="muted">名称、值、标签和采集时间均来自真实 API</span></div>
          </div>
          <div v-if="categoryMetrics(selected, selectedTab).length" class="table-scroll">
            <table class="fleet-table raw-metrics">
              <thead><tr><th>指标</th><th>资源</th><th>值</th><th>标签</th><th>采集时间</th></tr></thead>
              <tbody><tr v-for="point in categoryMetrics(selected, selectedTab)" :key="`${point.name}-${JSON.stringify(point.labels)}`">
                <td><code>{{ point.name }}</code></td>
                <td>{{ point.labels?.device || point.labels?.mount || point.labels?.app || point.labels?.sensor || '系统资源' }}</td>
                <td><b>{{ formatMetricValue(point.value, point.unit) }}</b></td>
                <td>{{ Object.entries(point.labels || {}).map(([key, value]) => `${key}=${value}`).join(', ') || '无标签' }}</td>
                <td>{{ ago(point.collectedAt) }}<small>{{ dateTime(point.collectedAt) }}</small></td>
              </tr></tbody>
            </table>
          </div>
          <div v-else class="inline-empty">当前没有此分类的采集指标。</div>
        </section>
        </div>
      </template>
    </PageState>
  </div>

  <PageState v-else :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro">
      <div><h2>设备</h2><p>用筛选和保存视图快速缩小范围；健康、连接、能力各自可筛选。</p></div>
    </div>
    <div class="saved-views"><b>保存视图</b><button @click="applyView({})">全部设备</button><button @click="applyView({ status: 'attention' })">需要处置</button><button @click="applyView({ capability: 'limited' })">能力受限</button><button @click="applyView({ connectivity: 'unavailable' })">离线或陈旧</button><button v-for="view in data?.savedViews" :key="view.id" @click="applyView(view.query)">{{ view.name }}</button></div>
    <div class="filter-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="按名称、位置或标签搜索"></label>
      <select v-model="statusFilter"><option value="all">健康状态</option><option value="critical">严重</option><option value="warning">警告</option><option value="healthy">健康</option><option value="offline">离线</option></select>
      <select v-model="connectivityFilter"><option value="all">连接状态</option><option value="online">在线</option><option value="stale">陈旧</option><option value="offline">离线</option></select>
      <select v-model="capabilityFilter"><option value="all">采集能力</option><option value="full">完整</option><option value="limited">受限</option></select>
      <select v-model="groupFilter"><option value="all">设备组</option><option v-for="group in groups" :key="group" :value="group">{{ group }}</option></select><button class="secondary-button" @click="saveView">保存当前视图</button>
    </div>
    <section class="card">
      <div class="section-title">
        <div><h2>设备清单</h2><span class="muted">已显示 {{ filteredDevices.length }} / {{ data?.devices.length ?? 0 }} 台设备 · 按严重度排序</span></div>
      </div>
      <DeviceTable v-if="filteredDevices.length" :items="filteredDevices" clickable @select="showDevice" />
      <div v-else class="inline-empty">没有符合当前筛选条件的设备。</div>
    </section>
  </PageState>
</template>
