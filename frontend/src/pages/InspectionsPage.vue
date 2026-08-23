<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Inspection } from '@/types'
import { signed } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'

const emit = defineEmits<{ toast: [message: string] }>()
const running = ref(false)
const { data, loading, error, refresh } = usePolling(() => api<{ items: Inspection[] }>('/api/v1/inspections'))
const latest = computed(() => data.value?.items[0])

async function start() {
  running.value = true
  try {
    await api('/api/v1/inspections', { method: 'POST' })
    emit('toast', '正式巡检已完成并保存')
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    running.value = false
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error">
    <div class="page-head">
      <div><h2>正式巡检</h2><span class="muted">每日 03:00、每周日 04:00 自动巡检；失败最多重试 3 次</span></div>
      <button :disabled="running" @click="start">{{ running ? '巡检中…' : '立即巡检' }}</button>
    </div>
    <div v-if="latest" class="stats four">
      <StatCard label="检查设备" :value="latest.deviceCount" hint="最近一次" />
      <StatCard label="健康" :value="latest.healthyCount" hint="通过" tone="green" />
      <StatCard label="Warning" :value="latest.warningCount" :hint="`变化 ${signed(latest.changeSummary?.warningDelta)}`" tone="amber" />
      <StatCard label="Critical" :value="latest.criticalCount" :hint="`变化 ${signed(latest.changeSummary?.criticalDelta)}`" tone="red" />
    </div>
    <div v-if="data?.items.length" class="card">
      <h2>巡检历史</h2>
      <table><thead><tr><th>时间/类型</th><th>设备</th><th>健康</th><th>Warning</th><th>Critical</th><th>变化</th><th>证据 SHA-256</th></tr></thead>
        <tbody><tr v-for="item in data.items" :key="item.id">
          <td>{{ new Date(item.startedAt).toLocaleString() }}<small>{{ item.triggerType }}</small></td>
          <td>{{ item.deviceCount }}</td><td class="green">{{ item.healthyCount }}</td><td class="amber">{{ item.warningCount }}</td><td class="red">{{ item.criticalCount }}</td>
          <td>新增 {{ item.changeSummary?.newAlerts?.length || 0 }} · 恢复 {{ item.changeSummary?.resolvedAlerts?.length || 0 }}</td>
          <td><code>{{ item.evidenceSha256.slice(0, 16) }}…</code></td>
        </tr></tbody>
      </table>
    </div>
    <div v-else class="card empty"><h2>尚无巡检记录</h2><p class="muted">自动巡检将在计划时间运行，也可以立即执行。</p></div>
  </PageState>
</template>
