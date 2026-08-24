<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Alert } from '@/types'
import { ago, formatMetricValue } from '@/utils'
import StatusPill from '@/components/StatusPill.vue'
import AppIcon from '@/components/AppIcon.vue'
import PageState from '@/components/PageState.vue'

const emit = defineEmits<{ toast: [message: string] }>()
const query = ref('')
const filter = ref('active')
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
  let matchesFilter = false
  if (filter.value === 'all') matchesFilter = true
  else if (filter.value === 'active') matchesFilter = alert.status !== 'resolved'
  else if (filter.value === 'critical' || filter.value === 'warning') {
    matchesFilter = alert.severity === filter.value && alert.status !== 'resolved'
  } else matchesFilter = alert.status === filter.value
  return matchesQuery && matchesFilter
}))
const selectedAlert = computed(() => filtered.value.find((item) => item.fingerprint === selectedFingerprint.value) || filtered.value[0])


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
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro"><div><h2>告警处置</h2><p>按生命周期组织工作，不把“已确认”误认为“已恢复”。</p></div></div>
    <div class="alert-filter-tabs">
      <button :class="{ active: filter === 'active' }" @click="filter = 'active'">触发中 <b>{{ counts.critical + counts.warning }}</b></button>
      <button :class="{ active: filter === 'acknowledged' }" @click="filter = 'acknowledged'">已确认 <b>{{ counts.acknowledged }}</b></button>
      <button :class="{ active: filter === 'silenced' }" @click="filter = 'silenced'">已静默 <b>{{ data?.items.filter((item) => item.status === 'silenced').length || 0 }}</b></button>
      <button :class="{ active: filter === 'resolved' }" @click="filter = 'resolved'">已恢复 <b>{{ counts.resolved }}</b></button>
    </div>
    <div class="filter-bar alert-search-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索规则、设备或证据"></label>
      <select><option>全部严重度</option></select><select><option>未分配</option></select><select><option>最近 24 小时</option></select>
      <button class="secondary-button" disabled>批量确认</button>
    </div>
    <p v-if="actionEvidence" class="operation-evidence" :class="actionEvidence.status" role="status">{{ actionEvidence.message }}</p>
    <div class="alert-workbench">
      <section class="card alert-list-panel">
        <div class="section-title compact"><div><h2>{{ filtered.length }} 个告警</h2></div></div>
        <div v-if="filtered.length" class="triage-list">
          <button v-for="alert in filtered" :key="alert.fingerprint" class="alert-list-item" :class="{ active: selectedAlert?.fingerprint === alert.fingerprint, critical: alert.severity === 'critical' }" @click="selectedFingerprint = alert.fingerprint">
            <i /><span><b>{{ alert.message || alert.resource }}</b><small>{{ alert.deviceName }} · {{ ago(alert.lastSeenAt || alert.observedAt || alert.collectedAt) }}</small></span><StatusPill :status="alert.severity" />
          </button>
        </div>
        <div v-else class="healthy-empty"><span>✓</span><b>当前筛选下没有告警</b><small>Empty 不等同于未采集能力已健康。</small></div>
      </section>
      <section v-if="selectedAlert" class="card alert-detail-panel">
        <div class="section-title"><div><h2>{{ selectedAlert.message }}</h2><span class="muted">规则：{{ selectedAlert.resource }} · 设备 {{ selectedAlert.deviceName }}</span></div><StatusPill :status="selectedAlert.status === 'firing' ? selectedAlert.severity : selectedAlert.status" /></div>
        <div class="evidence-box"><span>最新证据</span><strong>{{ selectedAlert.resource }} = {{ selectedAlert.unit ? formatMetricValue(selectedAlert.value, selectedAlert.unit) : '状态异常' }}</strong><small>采集于 {{ ago(selectedAlert.lastSeenAt || selectedAlert.observedAt || selectedAlert.collectedAt) }} · 来源：内置采集器</small></div>
        <div class="lifecycle"><b>生命周期</b><div><span class="done">触发中</span><span :class="{ done: selectedAlert.status === 'acknowledged' }">已确认</span><span>已恢复</span></div></div>
        <label class="owner-field"><span>负责人</span><input value="设备管理员" readonly></label>
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
