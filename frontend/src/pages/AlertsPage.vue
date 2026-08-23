<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Alert } from '@/types'
import AlertRow from '@/components/AlertRow.vue'
import AppIcon from '@/components/AppIcon.vue'
import PageState from '@/components/PageState.vue'

const emit = defineEmits<{ toast: [message: string] }>()
const query = ref('')
const filter = ref('active')
const { data, loading, error, refresh } = usePolling(() => api<{ items: Alert[] }>('/api/v1/alerts?includeResolved=true'))
const counts = computed(() => ({
  all: data.value?.items.length || 0,
  critical: data.value?.items.filter((item) => item.severity === 'critical' && item.status !== 'resolved').length || 0,
  warning: data.value?.items.filter((item) => item.severity === 'warning' && item.status !== 'resolved').length || 0,
  acknowledged: data.value?.items.filter((item) => item.status === 'acknowledged').length || 0,
  resolved: data.value?.items.filter((item) => item.status === 'resolved').length || 0,
}))
const filtered = computed(() => (data.value?.items || []).filter((alert) => {
  const matchesQuery = `${alert.deviceName} ${alert.resource} ${alert.message}`.toLowerCase().includes(query.value.trim().toLowerCase())
  const matchesFilter = filter.value === 'all'
    || (filter.value === 'active' && alert.status !== 'resolved')
    || alert.severity === filter.value
    || alert.status === filter.value
  return matchesQuery && matchesFilter
}))

async function action(fingerprint: string, name: string) {
  try {
    await api(`/api/v1/alerts/${encodeURIComponent(fingerprint)}/${name}`, {
      method: 'POST', body: name === 'silence' ? JSON.stringify({ durationMinutes: 1440 }) : undefined,
    })
    emit('toast', '告警状态已更新并回读')
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro"><div><h2>告警中心</h2><p>优先处理数据安全、服务不可用和持续资源压力。</p></div></div>
    <div class="alert-filter-tabs">
      <button :class="{ active: filter === 'all' }" @click="filter = 'all'">全部 <b>{{ counts.all }}</b></button>
      <button :class="{ active: filter === 'critical' }" @click="filter = 'critical'">Critical <b>{{ counts.critical }}</b></button>
      <button :class="{ active: filter === 'warning' }" @click="filter = 'warning'">Warning <b>{{ counts.warning }}</b></button>
      <button :class="{ active: filter === 'acknowledged' }" @click="filter = 'acknowledged'">已确认 <b>{{ counts.acknowledged }}</b></button>
      <button :class="{ active: filter === 'resolved' }" @click="filter = 'resolved'">已恢复 <b>{{ counts.resolved }}</b></button>
    </div>
    <div class="filter-bar alert-search-bar">
      <label class="search-field"><AppIcon name="search" :size="16" /><input v-model="query" placeholder="搜索设备、资源或告警"></label>
      <button class="secondary-button" @click="filter = 'active'">仅活动告警</button>
    </div>
    <section class="card triage-card">
      <div class="section-title"><div><h2>{{ filter === 'active' ? '活动告警' : '告警结果' }}</h2><span class="muted">{{ filtered.length }} 条 · 持久化状态机与 LazyCat 系统通知</span></div></div>
      <div v-if="filtered.length" class="triage-list">
        <AlertRow v-for="alert in filtered" :key="alert.fingerprint" :alert="alert" actionable @action="action" />
      </div>
      <div v-else class="healthy-empty"><span>✓</span><b>当前筛选下没有告警</b><small>Empty 不等同于未采集能力已健康。</small></div>
    </section>
  </PageState>
</template>
