<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling } from '@/composables'
import type { Alert } from '@/types'
import { ago, formatMetricValue } from '@/utils'
import StatusPill from '@/components/StatusPill.vue'
import AppIcon from '@/components/AppIcon.vue'
import AppPagination from '@/components/AppPagination.vue'
import PageState from '@/components/PageState.vue'
import BarChart from '@/components/BarChart.vue'
import DonutChart from '@/components/DonutChart.vue'
import { appConfirm } from '@/dialog'

const emit = defineEmits<{ toast: [message: string] }>()
const query = ref(sessionStorage.getItem('watchcatSearch') || '')
const filter = ref('active')
const severityFilter = ref('all')
const timeFilter = ref('168')
const actionEvidence = ref<{ status: 'success' | 'warning' | 'error'; message: string }>()
const actionLoading = ref('')
const selectedFingerprint = ref('')
const { data, loading, error, refresh } = usePolling(async () => {
  const result = await api<{ items: Alert[] | null }>('/api/v1/alerts?includeResolved=true')
  return { items: result.items || [] }
})
const counts = computed(() => ({
  all: data.value?.items.length || 0,
  critical: data.value?.items.filter((item) => item.severity === 'critical' && item.status !== 'resolved').length || 0,
  warning: data.value?.items.filter((item) => item.severity === 'warning' && item.status !== 'resolved').length || 0,
  acknowledged: data.value?.items.filter((item) => item.status === 'acknowledged').length || 0,
  resolved: data.value?.items.filter((item) => item.status === 'resolved').length || 0,
}))
const filtered = computed(() => (data.value?.items || []).filter((alert) => {
  const matchesQuery = `${alert.deviceName} ${alert.resource} ${alert.message}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const matchesSeverity = severityFilter.value === 'all' || alert.severity === severityFilter.value
  const observed = new Date(alert.lastSeenAt || alert.observedAt || alert.collectedAt || 0).getTime()
  const matchesTime = timeFilter.value === 'all' || observed >= Date.now() - Number(timeFilter.value) * 60 * 60 * 1000
  let matchesFilter = false
  if (filter.value === 'all') matchesFilter = true
  else if (filter.value === 'active') matchesFilter = alert.status !== 'resolved'
  else if (filter.value === 'critical' || filter.value === 'warning') {
    matchesFilter = alert.severity === filter.value && alert.status !== 'resolved'
  } else matchesFilter = alert.status === filter.value
  return matchesQuery && matchesFilter && matchesSeverity && matchesTime
}))
const alertPagination = usePagination(filtered, 20)
watch([query, filter, severityFilter, timeFilter], alertPagination.resetPage)
const selectedAlert = computed(() => filtered.value.find((item) => item.fingerprint === selectedFingerprint.value) || alertPagination.pagedItems.value[0])
watch(alertPagination.page, () => {
  selectedFingerprint.value = alertPagination.pagedItems.value[0]?.fingerprint || ''
})
const lifecycleDistribution = computed(() => [
  { label: '触发中', value: (data.value?.items || []).filter((item) => item.status === 'firing').length, color: '#c51d23' },
  { label: '已确认', value: counts.value.acknowledged, color: '#c05600' },
  { label: '已静默', value: (data.value?.items || []).filter((item) => item.status === 'silenced').length, color: '#7c3aed' },
  { label: '已恢复', value: counts.value.resolved, color: '#118847' },
])
const severityByTime = computed(() => {
  const now = Date.now()
  const windows = [
    { label: '0–6h', from: 0, to: 6 },
    { label: '6–12h', from: 6, to: 12 },
    { label: '12–24h', from: 12, to: 24 },
    { label: '1–7d', from: 24, to: 168 },
  ]
  return windows.map((window) => ({
    label: window.label,
    value: (data.value?.items || []).filter((item) => {
      const observed = new Date(item.lastSeenAt || item.observedAt || item.collectedAt || 0).getTime()
      const hours = (now - observed) / 3_600_000
      return hours >= window.from && hours < window.to
    }).length,
    color: window.to <= 24 ? '#c51d23' : '#c05600',
  }))
})

function alertDiskName(alert: Alert) {
  const resource = String(alert.resource || '').replace(/^\/dev\//, '')
  return /^(sd[a-z]+|nvme\d+n\d+|mmcblk\d+)$/.test(resource) ? resource : ''
}
function openDiskDetail(alert: Alert) {
  const disk = alertDiskName(alert)
  if (!disk || !alert.deviceId) return
  location.hash = `storage?deviceId=${encodeURIComponent(alert.deviceId)}&disk=${encodeURIComponent(disk)}`
}

async function action(fingerprint: string, name: string) {
  if (actionLoading.value) return
  actionLoading.value = fingerprint
  actionEvidence.value = undefined
  try {
    const result = await api<{ fingerprint: string; status: string }>(`/api/v1/alerts/${encodeURIComponent(fingerprint)}/${name}`, {
      method: 'POST', body: name === 'silence' ? JSON.stringify({ durationMinutes: 1440 }) : undefined,
    })
    const refreshed = await refresh()
    const expected = name === 'acknowledge' ? 'acknowledged' : 'silenced'
    const current = refreshed?.items.find((item) => item.fingerprint === fingerprint)
    if (result.fingerprint === fingerprint && current?.status === expected) {
      actionEvidence.value = { status: 'success', message: `已回读确认告警状态：${expected === 'acknowledged' ? '已确认' : '已静默 24 小时'}` }
      emit('toast', '告警状态已更新并回读确认')
    } else {
      actionEvidence.value = { status: 'warning', message: '写入请求已返回，但告警列表回读尚未确认目标状态' }
      emit('toast', '告警状态回读尚未确认')
    }
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    actionEvidence.value = { status: 'error', message }
    emit('toast', message)
  } finally {
    actionLoading.value = ''
  }
}
async function bulkAcknowledge() {
  const fingerprints = filtered.value.filter((item) => item.status !== 'resolved').map((item) => item.fingerprint)
  if (!fingerprints.length || !await appConfirm({ title: '批量确认告警', message: `确认当前筛选下的 ${fingerprints.length} 个告警？`, confirmText: `确认 ${fingerprints.length} 个告警` })) return
  await api('/api/v1/alerts/bulk-acknowledge', { method: 'POST', body: JSON.stringify({ fingerprints }) })
  await refresh()
  emit('toast', `已确认 ${fingerprints.length} 个告警`)
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro"><div><h2>告警处置</h2></div></div>
    <div class="alert-filter-tabs">
      <button :class="{ active: filter === 'active' }" @click="filter = 'active'">触发中 <b>{{ counts.critical + counts.warning }}</b></button>
      <button :class="{ active: filter === 'acknowledged' }" @click="filter = 'acknowledged'">已确认 <b>{{ counts.acknowledged }}</b></button>
      <button :class="{ active: filter === 'silenced' }" @click="filter = 'silenced'">已静默 <b>{{ data?.items.filter((item) => item.status === 'silenced').length || 0 }}</b></button>
      <button :class="{ active: filter === 'resolved' }" @click="filter = 'resolved'">已恢复 <b>{{ counts.resolved }}</b></button>
    </div>
    <div class="filter-bar alert-search-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索规则、设备或证据"></label>
      <select v-model="severityFilter"><option value="all">全部严重度</option><option value="critical">严重</option><option value="warning">警告</option></select>
      <span class="filter-note" title="当前为单用户模式">负责人：设备管理员</span>
      <select v-model="timeFilter"><option value="168">最近 7 天</option><option value="720">最近 30 天</option><option value="all">全部时间</option></select>
      <button class="secondary-button" :disabled="!filtered.length" @click="bulkAcknowledge">批量确认</button>
    </div>
    <p v-if="actionEvidence" class="operation-evidence" :class="actionEvidence.status" role="status">{{ actionEvidence.message }}</p>
    <div class="chart-panel-grid">
      <section class="card">
        <div class="section-title"><div><h2>告警生命周期构成</h2></div></div>
        <DonutChart :items="lifecycleDistribution" center-label="告警" />
      </section>
      <section class="card">
        <div class="section-title"><div><h2>最近事件分布</h2></div></div>
        <BarChart :items="severityByTime" />
      </section>
    </div>
    <div class="alert-workbench">
      <section class="card alert-list-panel">
        <div class="section-title compact"><div><h2>{{ filtered.length }} 个告警</h2></div></div>
        <div v-if="filtered.length" class="triage-list">
          <button v-for="alert in alertPagination.pagedItems.value" :key="alert.fingerprint" class="alert-list-item" :class="{ active: selectedAlert?.fingerprint === alert.fingerprint, critical: alert.severity === 'critical' }" @click="selectedFingerprint = alert.fingerprint">
            <i /><span><b>{{ alert.message || alert.resource }}</b><small>{{ alert.deviceName }} · {{ ago(alert.lastSeenAt || alert.observedAt || alert.collectedAt) }}</small></span><StatusPill :status="alert.severity" />
          </button>
        </div>
        <div v-else class="healthy-empty"><span>✓</span><b>当前筛选下没有告警</b><small>Empty 不等同于未采集能力已健康。</small></div>
        <AppPagination v-model:page="alertPagination.page.value" v-model:page-size="alertPagination.pageSize.value" :total="alertPagination.total.value" :page-count="alertPagination.pageCount.value" :range-start="alertPagination.rangeStart.value" :range-end="alertPagination.rangeEnd.value" label="告警列表分页" />
      </section>
      <section v-if="selectedAlert" class="card alert-detail-panel">
        <div class="section-title"><div><h2>{{ selectedAlert.message }}</h2></div><StatusPill :status="selectedAlert.status === 'firing' ? selectedAlert.severity : selectedAlert.status" /></div>
        <div class="evidence-box"><span>最新证据</span><strong>{{ selectedAlert.resource }} = {{ selectedAlert.unit ? formatMetricValue(selectedAlert.value, selectedAlert.unit) : '状态异常' }}</strong><small>采集于 {{ ago(selectedAlert.lastSeenAt || selectedAlert.observedAt || selectedAlert.collectedAt) }} · 来源：内置采集器</small></div>
        <div class="lifecycle"><b>生命周期</b><div><span class="done">触发中</span><span :class="{ done: selectedAlert.status === 'acknowledged' }">已确认</span><span>已恢复</span></div></div>
        <label class="owner-field"><span>负责人</span><input value="设备管理员" readonly></label>
        <button v-if="alertDiskName(selectedAlert)" class="storage-alert-link" @click="openDiskDetail(selectedAlert)">
          <AppIcon name="storage" :size="17" />
          <span><b>查看 {{ alertDiskName(selectedAlert) }} 磁盘详情</b><small>定位到物理磁盘、型号、序列号、下属卷和历史趋势</small></span>
          <i>→</i>
        </button>
        <div v-if="selectedAlert.status !== 'resolved'" class="alert-detail-actions">
          <button class="secondary-button" :disabled="Boolean(actionLoading)" @click="action(selectedAlert.fingerprint, 'silence')">静默通知</button>
          <button class="primary-button" :disabled="Boolean(actionLoading)" @click="action(selectedAlert.fingerprint, 'acknowledge')">确认并回读</button>
        </div>
        <div class="recovery-note"><b>当前不能恢复</b><span>规则仍成立；恢复将由系统在证据低于恢复阈值后自动产生。</span></div>
        <a class="section-link" href="#settings">查看完整审计记录 →</a>
      </section>
    </div>
  </PageState>
</template>
