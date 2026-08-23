<script setup lang="ts">
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Alert } from '@/types'
import AlertRow from '@/components/AlertRow.vue'
import PageState from '@/components/PageState.vue'

const emit = defineEmits<{ toast: [message: string] }>()
const { data, loading, error, refresh } = usePolling(() => api<{ items: Alert[] }>('/api/v1/alerts'))

async function action(fingerprint: string, name: string) {
  try {
    await api(`/api/v1/alerts/${encodeURIComponent(fingerprint)}/${name}`, {
      method: 'POST',
      body: name === 'silence' ? JSON.stringify({ durationMinutes: 1440 }) : undefined,
    })
    emit('toast', '告警状态已更新')
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="没有活动告警" empty-text="当前设备均未达到告警阈值。">
    <div class="page-head"><div><h2>告警中心</h2><span class="muted">持久化状态机 · 新发/升级/恢复通过 LazyCat 系统通知</span></div></div>
    <div class="card alert-list">
      <AlertRow v-for="alert in data?.items" :key="alert.fingerprint" :alert="alert" actionable @action="action" />
    </div>
  </PageState>
</template>
