<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Metric, Overview } from '@/types'
import { ago, dateTime, deviceState, formatNumber, metricValueAny, statusRank } from '@/utils'
import AppIcon from '@/components/AppIcon.vue'
import DeviceTable from '@/components/DeviceTable.vue'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

const emit = defineEmits<{ toast: [message: string] }>()
type DetailTab = 'overview' | 'system' | 'storage' | 'apps' | 'network' | 'events'
const detailTabs: Array<[DetailTab, string]> = [
  ['overview', '概览'], ['system', '系统'], ['storage', '存储与硬件'],
  ['apps', '应用与容器'], ['network', '网络'], ['events', '事件'],
]

const selected = ref<Device>()
const selectedTab = ref<DetailTab>('overview')
const detailLoading = ref(false)
const detailError = ref('')
const query = ref('')
const statusFilter = ref('all')
const { data, loading, error, refresh } = usePolling(() => api<Overview>('/api/v1/overview'))

const filteredDevices = computed(() => (data.value?.devices || [])
  .filter((device) => {
    const text = `${device.name} ${device.hostname} ${device.osVersion}`.toLowerCase()
    const matchesQuery = text.includes(query.value.trim().toLowerCase())
    const matchesStatus = statusFilter.value === 'all' || deviceState(device) === statusFilter.value
    return matchesQuery && matchesStatus
  })
  .sort((a, b) => statusRank(deviceState(a)) - statusRank(deviceState(b))))

async function showDevice(id: string) {
  detailLoading.value = true
  detailError.value = ''
  selectedTab.value = 'overview'
  try {
    selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(id)}`)
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  selected.value = undefined
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
</script>

<template>
  <div v-if="selected || detailLoading || detailError">
    <button class="back-button" @click="closeDetail"><AppIcon name="arrow-left" :size="16" /> 返回设备清单</button>
    <PageState :loading="detailLoading" :error="detailError" @retry="selected && showDevice(selected.id)">
      <template v-if="selected">
        <section class="device-hero">
          <div>
            <div class="device-title-line"><h2>{{ selected.name }}</h2><StatusPill :status="deviceState(selected)" /></div>
            <p>{{ selected.hostname || '主机名未知' }} · 未分组 · 最后上报 {{ ago(selected.lastSeenAt) }}</p>
            <span>{{ selected.osVersion || 'LazyCat OS 版本未知' }} · Collector {{ selected.collectorVersion || 'Unknown' }}</span>
          </div>
          <div class="button-row">
            <button class="secondary-button" disabled title="当前 API 未提供设备编辑接口">编辑设备</button>
            <button class="primary-button" @click="emit('toast', '请从全局“开始巡检”进入正式巡检')">运行巡检</button>
          </div>
        </section>

        <div class="tab-bar" role="tablist">
          <button v-for="[key, label] in detailTabs" :key="key" :class="{ active: selectedTab === key }" role="tab" @click="selectedTab = key">{{ label }}</button>
        </div>

        <template v-if="selectedTab === 'overview'">
          <div class="stats four detail-stats">
            <StatCard label="CPU" :value="metricValueAny(selected, ['system.cpu.usage'])" :hint="`Load ${metricValueAny(selected, ['system.load.1m'], 2)}`" />
            <StatCard label="内存" :value="metricValueAny(selected, ['system.memory.usage'])" hint="实时使用率" />
            <StatCard label="存储" :value="metricValueAny(selected, ['filesystem.root.usage', 'btrfs.usage'])" hint="根文件系统 / Pool" />
            <StatCard label="Uptime" :value="metricValueAny(selected, ['system.uptime'], 0)" hint="运行时长（API 原始单位）" />
          </div>
          <div class="detail-layout">
            <section class="card">
              <div class="section-title"><div><h2>运行概况</h2><span class="muted">当前设备最新有效快照</span></div></div>
              <div class="metric-summary-grid">
                <div><span>连接状态</span><StatusPill :status="selected.online ? selected.stale ? 'stale' : 'healthy' : 'offline'" /></div>
                <div><span>健康状态</span><StatusPill :status="selected.health || 'unknown'" /></div>
                <div><span>注册状态</span><b>{{ selected.status || 'Unknown' }}</b></div>
                <div><span>最后上报</span><b>{{ dateTime(selected.lastSeenAt) }}</b></div>
              </div>
            </section>
            <section class="card">
              <div class="section-title compact"><div><h2>Fleet 元数据</h2><span class="muted">设备资产信息</span></div></div>
              <dl class="definition-list">
                <div><dt>设备组</dt><dd>Unknown · Contract gap</dd></div>
                <div><dt>标签</dt><dd>Unknown · Contract gap</dd></div>
                <div><dt>位置</dt><dd>Unknown · Contract gap</dd></div>
                <div><dt>设备 ID</dt><dd><code>{{ selected.id }}</code></dd></div>
              </dl>
            </section>
          </div>
        </template>

        <section v-else-if="selectedTab === 'events'" class="card">
          <div class="section-title"><div><h2>设备事件</h2><span class="muted">告警与审计事件</span></div></div>
          <div class="contract-empty"><b>Contract gap</b><p>当前设备详情 API 未提供独立事件流。请在“告警”和“巡检”页面查看现有证据。</p></div>
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
                <td><b>{{ formatNumber(point.value) }}{{ point.unit || '' }}</b></td>
                <td>{{ Object.entries(point.labels || {}).map(([key, value]) => `${key}=${value}`).join(', ') || '无标签' }}</td>
                <td>{{ ago(point.collectedAt) }}<small>{{ dateTime(point.collectedAt) }}</small></td>
              </tr></tbody>
            </table>
          </div>
          <div v-else class="contract-empty"><b>Unknown</b><p>当前 Collector 尚未提供此分类的指标，不能据此判断为健康。</p></div>
        </section>
      </template>
    </PageState>
  </div>

  <PageState v-else :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro">
      <div><h2>设备清单</h2><p>集中查看在线状态、系统版本、资源压力与应用异常。</p></div>
      <div class="button-row"><button class="secondary-button" disabled>批量巡检</button><button class="primary-button" disabled>＋ 接入设备</button></div>
    </div>
    <div class="filter-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索设备名称、主机名或系统版本"></label>
      <select disabled title="当前 API 未提供设备组"><option>设备组</option></select>
      <select v-model="statusFilter"><option value="all">全部状态</option><option value="critical">Critical</option><option value="warning">Warning</option><option value="healthy">Healthy</option><option value="offline">Offline</option></select>
      <select disabled title="当前 API 未提供版本分布筛选"><option>系统版本</option></select>
      <select disabled title="当前 API 未提供标签"><option>标签</option></select>
    </div>
    <section class="card">
      <div class="section-title">
        <div><h2>全部设备</h2><span class="muted">{{ filteredDevices.length }} / {{ data?.devices.length ?? 0 }} 台 · 按严重度排序</span></div>
      </div>
      <DeviceTable v-if="filteredDevices.length" :items="filteredDevices" clickable @select="showDevice" />
      <div v-else class="inline-empty">没有符合当前筛选条件的设备。</div>
    </section>
  </PageState>
</template>
