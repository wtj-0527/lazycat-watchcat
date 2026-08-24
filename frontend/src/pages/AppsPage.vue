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
const query = ref(sessionStorage.getItem('maoyanSearch') || '')
const statusFilter = ref('all')
const viewMode = ref<'app' | 'device'>('app')
const tableMode = ref(false)
const selectedApp = ref<ApplicationItem>()
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
const statusLabel = (status: string) => ({ running: '健康', paused: '暂停', starting: '启动中', stopping: '停止中', error: '严重' } as Record<string, string>)[status] || status || '未知'
const deviceColumns = computed(() => {
  const devices = new Map<string, string>()
  for (const item of data.value?.items || []) for (const device of item.devices || []) devices.set(device.deviceId, device.deviceName || '未知设备')
  return [...devices.entries()].map(([id, name]) => ({ id, name }))
})
function instanceFor(item: ApplicationItem, deviceId: string) { return item.devices?.find((device) => device.deviceId === deviceId) }
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="LazyCat Package Manager 尚未返回当前用户的应用状态。" @retry="refresh">
    <div class="page-intro">
      <div><h2>应用矩阵</h2><p>从应用视角判断影响范围；矩阵允许组件内部横向滚动，不让页面根节点溢出。</p></div>
      <div class="view-toggle"><button :class="{ active: viewMode === 'app' }" @click="viewMode = 'app'">按应用</button><button :class="{ active: viewMode === 'device' }" @click="viewMode = 'device'">按设备</button></div>
    </div>
    <div class="filter-bar app-filter-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索应用名称"></label>
      <select v-model="statusFilter"><option value="all">全部状态</option><option value="healthy">运行正常</option><option value="degraded">已暂停</option><option value="critical">异常</option></select>
      <span class="pill critical">异常 {{ errors }}</span><span class="pill warning">已暂停 {{ paused }}</span>
      <span class="filter-note">更新 {{ ago(data?.updatedAt) }}</span>
    </div>
    <section class="card app-matrix-card">
      <div class="section-title"><div><h2>{{ viewMode === 'app' ? '应用 × 设备' : '设备 × 应用' }}</h2><span class="muted">颜色、图形和文字共同表达状态；未知状态不计入健康率。</span></div><button class="row-link" @click="tableMode = !tableMode">{{ tableMode ? '切换为矩阵视图' : '切换为表格视图' }}</button></div>
      <div class="table-scroll">
        <table v-if="tableMode" class="fleet-table">
          <thead><tr><th>应用</th><th>设备</th><th>状态</th><th>版本</th><th>CPU</th><th>内存</th></tr></thead>
          <tbody><template v-for="item in filtered" :key="item.id"><tr v-for="instance in item.devices" :key="instance.deployId" class="device-row" @click="selectedApp = item">
            <td><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small></td><td>{{ instance.deviceName }}</td><td><StatusPill :status="instance.status === 'running' ? 'healthy' : instance.status === 'error' ? 'critical' : 'warning'" /></td><td>{{ instance.version }}</td><td>{{ item.resources.cpuPercent.toFixed(1) }}%</td><td>{{ bytes(item.resources.memoryUsage) }}</td>
          </tr></template></tbody>
        </table>
        <table v-else-if="viewMode === 'app'" class="fleet-table app-matrix">
          <thead><tr><th>应用</th><th v-for="device in deviceColumns" :key="device.id">{{ device.name }}</th><th v-if="!deviceColumns.length">当前设备</th></tr></thead>
          <tbody>
            <tr v-for="item in filtered" :key="item.id">
              <td class="device"><b>{{ item.title || item.id }}</b><small>{{ item.id }} · {{ Object.keys(item.versions).join(' / ') || '版本未知' }}</small></td>
              <td v-for="device in deviceColumns" :key="device.id">
                <button v-if="instanceFor(item, device.id)" class="matrix-cell" :class="instanceFor(item, device.id)?.status" @click="selectedApp = item">
                  <i /> <b>{{ statusLabel(instanceFor(item, device.id)?.status || '') }}</b>
                  <small>{{ instanceFor(item, device.id)?.version || '版本未知' }}</small>
                </button>
                <div v-else class="matrix-cell unknown"><i /><b>未知</b><small>尚无实例证据</small></div>
              </td>
              <td v-if="!deviceColumns.length"><div class="matrix-cell unknown"><i /><b>未知</b><small>尚无设备实例</small></div></td>
            </tr>
          </tbody>
        </table>
        <table v-else class="fleet-table app-matrix">
          <thead><tr><th>设备</th><th v-for="item in filtered" :key="item.id">{{ item.title || item.id }}</th></tr></thead>
          <tbody><tr v-for="device in deviceColumns" :key="device.id">
            <td class="device"><b>{{ device.name }}</b><small>{{ device.id }}</small></td>
            <td v-for="item in filtered" :key="item.id">
              <button v-if="instanceFor(item, device.id)" class="matrix-cell" :class="instanceFor(item, device.id)?.status" @click="selectedApp = item"><i /><b>{{ statusLabel(instanceFor(item, device.id)?.status || '') }}</b><small>{{ instanceFor(item, device.id)?.version }}</small></button>
              <div v-else class="matrix-cell unknown"><i /><b>未安装</b></div>
            </td>
          </tr></tbody>
        </table>
      </div>
      <div v-if="!filtered.length" class="inline-empty">没有符合当前筛选条件的应用。</div>
    </section>
    <section v-if="selectedApp" class="matrix-context">
      <div><h2>{{ selectedApp.title || selectedApp.id }}</h2><p>{{ selectedApp.id }} · {{ selectedApp.instances }} 个实例 · CPU {{ selectedApp.resources.cpuPercent.toFixed(1) }}% · 内存 {{ bytes(selectedApp.resources.memoryUsage) }}</p></div>
      <span>容器 {{ selectedApp.resources.containers }}</span><span>网络接收 {{ bytes(selectedApp.resources.networkReceive) }}</span><button class="row-link" @click="selectedApp = undefined">关闭</button>
    </section>
  </PageState>
</template>
