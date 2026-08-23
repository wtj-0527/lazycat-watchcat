<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Capability, Metric } from '@/types'
import { ago, formatNumber, metricLabel } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Metric[]; updatedAt: string; capabilities: Capability[] }
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [storage, operations] = await Promise.all([
    api<{ items: Metric[]; updatedAt: string }>('/api/v1/storage'),
    api<{ capabilities: Capability[] }>('/api/v1/operations'),
  ])
  return { ...storage, capabilities: operations.capabilities }
})
const groups = computed(() => {
  const result: Record<string, Metric[]> = {}
  for (const item of data.value?.items || []) (result[item.deviceId || 'unknown'] ||= []).push(item)
  return Object.values(result)
})
const disks = computed(() => new Set((data.value?.items || []).filter((item) => item.name.startsWith('disk.')).map((item) => `${item.deviceId}:${item.labels?.device || item.labels?.sensor || item.name}`)).size)
const riskItems = computed(() => (data.value?.items || []).filter((item) =>
  (item.name.endsWith('.usage') && item.value >= 85)
  || (item.name === 'disk.nvme.media_errors' && item.value > 0)
  || (item.name === 'disk.temperature' && item.value >= 85),
).sort((a, b) => b.value - a.value))
const critical = computed(() => riskItems.value.filter((item) =>
  (item.name.endsWith('.usage') && item.value >= 95)
  || (item.name === 'disk.nvme.media_errors' && item.value > 0)
  || (item.name === 'disk.temperature' && item.value >= 90),
).length)
const find = (items: Metric[], names: string[]) => items.find((item) => names.some((name) => item.name === name || item.name.endsWith(name)))
const capabilityStatus = (name: string) => data.value?.capabilities.find((item) => item.capability.includes(name))
const riskStatus = (item: Metric) => ((item.name.endsWith('.usage') && item.value >= 95) || (item.name === 'disk.nvme.media_errors' && item.value > 0) || (item.name === 'disk.temperature' && item.value >= 90)) ? 'critical' : 'warning'
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="基础文件系统指标会自动上报；SMART 与 Btrfs 需要对应工具及只读权限。" @retry="refresh">
    <div class="page-intro"><div><h2>Fleet 存储健康</h2><p>按数据风险排序，不用平均值掩盖热点。</p></div><span class="muted">更新 {{ ago(data?.updatedAt) }}</span></div>
    <div class="stats four">
      <StatCard label="物理磁盘" :value="disks || 'Unknown'" hint="基于已上报磁盘标签" />
      <StatCard label="总容量" value="Unknown" hint="当前 API 未提供 Fleet 容量聚合" />
      <StatCard label="Critical" :value="critical" hint="需要立即处理" :tone="critical ? 'red' : 'green'" />
      <StatCard label="30 天内写满" value="Unknown" hint="缺少历史增长率契约" tone="amber" />
    </div>

    <section class="card storage-risk-card">
      <div class="section-title"><div><h2>存储风险优先级</h2><span class="muted">容量、温度和 Media Errors 使用独立风险语义</span></div></div>
      <div v-if="riskItems.length" class="table-scroll">
        <table class="fleet-table"><thead><tr><th>设备</th><th>资源</th><th>风险</th><th>当前值</th><th>采集时间</th><th>建议</th></tr></thead>
          <tbody><tr v-for="item in riskItems" :key="`${item.deviceId}-${item.name}-${metricLabel(item)}`">
            <td class="device"><b>{{ item.deviceName || '未知设备' }}</b><small>{{ item.deviceId }}</small></td>
            <td>{{ metricLabel(item) }}<small><code>{{ item.name }}</code></small></td>
            <td><StatusPill :status="riskStatus(item)" /></td>
            <td><b>{{ formatNumber(item.value) }}{{ item.unit }}</b></td>
            <td>{{ ago(item.collectedAt) }}</td>
            <td>{{ item.name.includes('media_errors') ? '检查 NVMe 健康与备份' : item.name.includes('temperature') ? '检查散热和负载' : '清理空间或扩容' }}</td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有达到阈值的存储风险</b><small>这不代表未上报能力为健康，请同时检查能力状态。</small></div></div>
    </section>

    <div class="storage-grid">
      <section v-for="items in groups" :key="items[0]?.deviceId" class="card storage-device-card">
        <div class="section-title compact"><div><h2>{{ items[0]?.deviceName || '未知设备' }}</h2><span class="muted">{{ items.length }} 项存储证据</span></div><StatusPill :status="items.some((item) => riskStatus(item) === 'critical') ? 'critical' : items.some((item) => riskItems.includes(item)) ? 'warning' : 'healthy'" /></div>
        <div class="storage-measure"><span>根文件系统</span><b>{{ find(items, ['filesystem.root.usage', 'btrfs.usage']) ? `${formatNumber(find(items, ['filesystem.root.usage', 'btrfs.usage'])?.value)}${find(items, ['filesystem.root.usage', 'btrfs.usage'])?.unit}` : 'Unknown' }}</b></div>
        <div class="storage-measure"><span>磁盘温度</span><b>{{ find(items, ['disk.temperature']) ? `${formatNumber(find(items, ['disk.temperature'])?.value, 0)}${find(items, ['disk.temperature'])?.unit || '°C'}` : 'Unknown' }}</b></div>
        <div class="storage-measure"><span>NVMe Media Errors</span><b>{{ find(items, ['disk.nvme.media_errors']) ? formatNumber(find(items, ['disk.nvme.media_errors'])?.value, 0) : 'Unknown' }}</b></div>
        <p class="muted">最近采集 {{ ago(items[0]?.collectedAt) }}</p>
      </section>
    </div>

    <section class="card capability-card">
      <div class="section-title"><div><h2>存储采集能力</h2><span class="muted">Restricted、Unsupported 与 Error 分开呈现</span></div></div>
      <div class="capability-grid">
        <div v-for="name in ['filesystem', 'btrfs', 'smart', 'nvme']" :key="name">
          <span>{{ name.toUpperCase() }}</span>
          <StatusPill :status="capabilityStatus(name)?.status || 'unknown'" />
          <small>{{ capabilityStatus(name)?.detail || '当前 API 未返回此能力状态' }}</small>
        </div>
      </div>
    </section>
  </PageState>
</template>
