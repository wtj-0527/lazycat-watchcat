<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Metric } from '@/types'
import { ago, formatNumber } from '@/utils'
import PageState from '@/components/PageState.vue'

const { data, loading, error } = usePolling(() => api<{ items: Metric[]; updatedAt: string }>('/api/v1/storage'))
const groups = computed(() => {
  const result: Record<string, Metric[]> = {}
  for (const item of data.value?.items || []) (result[item.deviceId || 'unknown'] ||= []).push(item)
  return Object.values(result)
})
const find = (items: Metric[], name: string) => items.find(item => item.name === name || item.name.endsWith(name))
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="基础文件系统指标会自动上报；SMART 与 Btrfs 需要对应工具及只读权限。">
    <div class="page-head"><div><h2>Fleet 存储健康</h2><span class="muted">{{ data?.items.length }} 项实时存储指标 · 更新 {{ ago(data?.updatedAt) }}</span></div></div>
    <div class="storage-grid">
      <div v-for="items in groups" :key="items[0]?.deviceId" class="card">
        <h2>{{ items[0]?.deviceName }}</h2><p class="muted">{{ items[0]?.labels?.mount || items[0]?.labels?.device || '系统存储' }}</p>
        <div v-if="find(items, '.usage')" class="risk"><b>使用率<span>{{ formatNumber(find(items, '.usage')?.value) }}{{ find(items, '.usage')?.unit }}</span></b></div>
        <div v-if="find(items, 'disk.temperature')" class="risk"><b>温度<span>{{ formatNumber(find(items, 'disk.temperature')?.value, 0) }}°C</span></b></div>
        <div v-if="find(items, 'disk.nvme.media_errors')" class="risk"><b>Media Errors<span :class="{ red: find(items, 'disk.nvme.media_errors')?.value }">{{ formatNumber(find(items, 'disk.nvme.media_errors')?.value, 0) }}</span></b></div>
        <p class="muted">更新 {{ ago(items[0]?.collectedAt) }}</p>
      </div>
    </div>
  </PageState>
</template>
