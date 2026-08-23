<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import { formatNumber } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'

interface AppItem { id: string; instances: number; healthy: number; unhealthy: number; versions: Record<string, number> }
const { data, loading, error } = usePolling(() => api<{ items: AppItem[] }>('/api/v1/applications'))
const instances = computed(() => data.value?.items.reduce((sum, item) => sum + item.instances, 0) ?? 0)
const healthy = computed(() => data.value?.items.reduce((sum, item) => sum + item.healthy, 0) ?? 0)
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="Collector 尚未获得 LazyCat Runtime 应用状态，或设备还未上报。">
    <div class="stats four">
      <StatCard label="LPK 应用" :value="data?.items.length ?? 0" hint="已发现" />
      <StatCard label="设备实例" :value="instances" hint="实时采集" />
      <StatCard label="运行正常" :value="healthy" :hint="instances ? `${formatNumber(healthy / instances * 100)}%` : '无数据'" tone="green" />
      <StatCard label="异常" :value="instances - healthy" hint="需要关注" :tone="instances - healthy ? 'amber' : 'green'" />
    </div>
    <div class="card">
      <h2>应用矩阵</h2>
      <table><thead><tr><th>应用</th><th>实例</th><th>正常</th><th>异常</th><th>版本</th></tr></thead>
        <tbody><tr v-for="item in data?.items" :key="item.id">
          <td><b>{{ item.id }}</b></td><td>{{ item.instances }}</td><td class="green">{{ item.healthy }}</td>
          <td :class="{ red: item.unhealthy }">{{ item.unhealthy }}</td>
          <td>{{ Object.entries(item.versions).map(([version, count]) => `${version || '未知'} × ${count}`).join(' · ') }}</td>
        </tr></tbody>
      </table>
    </div>
  </PageState>
</template>
