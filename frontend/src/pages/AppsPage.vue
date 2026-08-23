<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import { ago, bytes, formatNumber } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'

interface AppItem {
  id: string
  title: string
  instances: number
  healthy: number
  unhealthy: number
  paused: number
  versions: Record<string, number>
  statusCounts: Record<string, number>
  resources: {
    containers: number
    cpuPercent: number
    memoryUsage: number
    memoryLimit: number
    networkReceive: number
    networkTransmit: number
    blockRead: number
    blockWrite: number
    updatedAt?: string
  }
}
const { data, loading, error } = usePolling(() => api<{ items: AppItem[]; source: string; stale: boolean }>('/api/v1/applications'))
const instances = computed(() => data.value?.items.reduce((sum, item) => sum + item.instances, 0) ?? 0)
const healthy = computed(() => data.value?.items.reduce((sum, item) => sum + item.healthy, 0) ?? 0)
const paused = computed(() => data.value?.items.reduce((sum, item) => sum + item.paused, 0) ?? 0)
const errors = computed(() => data.value?.items.reduce((sum, item) => sum + item.unhealthy, 0) ?? 0)
const statusLabel = (status: string) => ({ running: '运行中', paused: '已暂停', starting: '启动中', stopping: '停止中', error: '异常' } as Record<string, string>)[status] || status
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无应用数据" empty-text="LazyCat Package Manager 尚未返回当前用户的应用状态。">
    <div class="stats four">
      <StatCard label="LPK 应用" :value="data?.items.length ?? 0" hint="已安装" />
      <StatCard label="运行中" :value="healthy" :hint="instances ? `${formatNumber(healthy / instances * 100)}%` : '无数据'" tone="green" />
      <StatCard label="已暂停" :value="paused" hint="非异常状态" tone="amber" />
      <StatCard label="异常" :value="errors" hint="需要关注" :tone="errors ? 'red' : 'green'" />
    </div>
    <div class="card">
      <div class="section-title"><div><h2>应用矩阵</h2><span class="muted">来源：LazyCat Package Manager API<span v-if="data?.stale"> · 当前显示最近一次成功快照</span></span></div></div>
      <table><thead><tr><th>应用</th><th>状态</th><th>容器资源</th><th>内存</th><th>网络 / 块 IO</th><th>版本</th></tr></thead>
        <tbody><tr v-for="item in data?.items" :key="item.id">
          <td class="device"><b>{{ item.title || item.id }}</b><small>{{ item.id }}</small></td>
          <td><span v-for="([status, count]) in Object.entries(item.statusCounts)" :key="status" class="pill runtime-status" :class="status">{{ statusLabel(status) }} × {{ count }}</span></td>
          <td>
            <template v-if="item.resources.containers">
              {{ item.resources.containers }} 容器 · CPU {{ formatNumber(item.resources.cpuPercent) }}%
              <small class="resource-time">{{ ago(item.resources.updatedAt) }}</small>
            </template>
            <span v-else class="muted">未授权容器指标</span>
          </td>
          <td>{{ item.resources.containers ? `${bytes(item.resources.memoryUsage)} / ${bytes(item.resources.memoryLimit)}` : '—' }}</td>
          <td>
            <template v-if="item.resources.containers">
              ↓ {{ bytes(item.resources.networkReceive) }} / ↑ {{ bytes(item.resources.networkTransmit) }}
              <small class="resource-time">读 {{ bytes(item.resources.blockRead) }} / 写 {{ bytes(item.resources.blockWrite) }}</small>
            </template>
            <span v-else>—</span>
          </td>
          <td>{{ Object.entries(item.versions).map(([version, count]) => `${version || '未知'} × ${count}`).join(' · ') }}</td>
        </tr></tbody>
      </table>
    </div>
  </PageState>
</template>
