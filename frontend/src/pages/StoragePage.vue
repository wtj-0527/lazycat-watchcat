<script setup lang="ts">
import { computed } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Capability, Metric } from '@/types'
import { ago, bytes, formatMetricValue, metricLabel, storageRiskAdvice, storageRiskStatus } from '@/utils'
import PageState from '@/components/PageState.vue'
import BarChart from '@/components/BarChart.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Metric[]; updatedAt: string; capabilities: Capability[]; summary: { totalBytes: number; fillWithin30Days: number }; history: Metric[]; historyTarget?: Metric }
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [storage, operations] = await Promise.all([
    api<{ items: Metric[] | null; updatedAt: string; summary: { totalBytes: number; fillWithin30Days: number } }>('/api/v1/storage'),
    api<{ capabilities: Capability[] | null }>('/api/v1/operations')
      .catch(() => ({ capabilities: null })),
  ])
  const items = storage.items || []
  const historyTarget = [...items]
    .filter((item) => item.name === 'filesystem.root.usage' || item.name === 'btrfs.usage')
    .sort((a, b) => b.value - a.value)[0]
  const history = historyTarget?.deviceId
    ? await api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(historyTarget.deviceId)}/metrics?name=${encodeURIComponent(historyTarget.name)}&hours=336`).then((result) => result.items || []).catch(() => [])
    : []
  return { ...storage, items, history, historyTarget, summary: storage.summary || { totalBytes: 0, fillWithin30Days: 0 }, capabilities: operations.capabilities || [] }
})
const groups = computed(() => {
  const result: Record<string, Metric[]> = {}
  for (const item of data.value?.items || []) (result[item.deviceId || 'unknown'] ||= []).push(item)
  return Object.values(result)
})
const disks = computed(() => new Set((data.value?.items || []).filter((item) => item.name.startsWith('disk.')).map((item) => `${item.deviceId}:${item.labels?.device || item.labels?.sensor || item.name}`)).size)
const riskStatus = (item: Metric) => item.risk || storageRiskStatus(item)
const riskItems = computed(() => (data.value?.items || [])
  .filter((item) => riskStatus(item))
  .sort((a, b) => {
    const severity = Number(riskStatus(a) === 'warning') - Number(riskStatus(b) === 'warning')
    return severity || b.value - a.value
  }))
const critical = computed(() => riskItems.value.filter((item) => riskStatus(item) === 'critical').length)
const find = (items: Metric[], names: string[]) => items.find((item) => names.some((name) => item.name === name || item.name.endsWith(name)))
const display = (items: Metric[], names: string[], digits = 1) => {
  const point = find(items, names)
  return point ? formatMetricValue(point.value, point.unit, digits) : '暂无数据'
}
const capabilityStatus = (name: string) => data.value?.capabilities.find((item) => item.capability.includes(name))
const capacityTrend = computed<ChartSeries[]>(() => {
  const target = data.value?.historyTarget
  const points = (data.value?.history || []).filter((point) => {
    if (!target) return false
    return (point.labels?.mount || '') === (target.labels?.mount || '') && (point.labels?.device || '') === (target.labels?.device || '')
  })
  return points.length ? [{
    name: `${target?.deviceName || '设备'} · ${target?.labels?.mount || target?.labels?.device || '主存储'}`,
    color: '#2563eb',
    points: points.map((point) => ({
      value: point.value,
      at: new Date(point.collectedAt).toLocaleString('zh-CN'),
      label: new Date(point.collectedAt).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }),
    })),
  }] : []
})
const temperatureBars = computed(() => (data.value?.items || [])
  .filter((item) => item.name === 'disk.temperature')
  .sort((a, b) => b.value - a.value)
  .slice(0, 8)
  .map((item) => ({
    label: `${item.deviceName || '设备'} / ${item.labels?.device || item.labels?.sensor || '磁盘'}`,
    value: Math.round(item.value),
    color: item.value >= 55 ? '#c51d23' : item.value >= 45 ? '#c05600' : '#2563eb',
    hint: `采集于 ${ago(item.collectedAt)}`,
  })))
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="基础文件系统指标会自动上报；SMART 与 Btrfs 需要对应工具及只读权限。" @retry="refresh">
    <div class="page-intro"><div><h2>Fleet 存储健康</h2><p>按数据风险排序，不用平均值掩盖热点。</p></div><span class="muted">更新 {{ ago(data?.updatedAt) }}</span></div>
    <div class="stats four">
      <StatCard label="物理磁盘" :value="disks" hint="基于实时磁盘标签" />
      <StatCard label="总容量" :value="bytes(data?.summary.totalBytes || 0)" hint="基于文件系统可用量与使用率计算" />
      <StatCard label="Critical" :value="critical" hint="需要立即处理" :tone="critical ? 'red' : 'green'" />
      <StatCard label="30 天内写满" :value="data?.summary.fillWithin30Days || 0" hint="基于最近 30 天真实增长率" tone="amber" />
    </div>

    <div class="chart-panel-grid">
      <section class="card">
        <div class="section-title"><div><h2>高风险卷 · 14 天使用率趋势</h2><span class="muted">自动选择当前使用率最高且具备历史证据的卷</span></div></div>
        <LineChart :series="capacityTrend" :min="0" :max="100" unit="%" :height="230" />
      </section>
      <section class="card">
        <div class="section-title"><div><h2>磁盘温度</h2><span class="muted">SMART 实时温度，颜色随阈值变化</span></div></div>
        <BarChart :items="temperatureBars" unit="°C" />
      </section>
    </div>

    <section class="card storage-risk-card">
      <div class="section-title"><div><h2>存储风险优先级</h2><span class="muted">容量、温度、NVMe 与 ATA 健康证据使用后端告警规则</span></div></div>
      <div v-if="riskItems.length" class="table-scroll">
        <table class="fleet-table"><thead><tr><th>设备</th><th>资源</th><th>风险</th><th>当前值</th><th>采集时间</th><th>建议</th></tr></thead>
          <tbody><tr v-for="item in riskItems" :key="`${item.deviceId}-${item.name}-${metricLabel(item)}`">
            <td class="device"><b>{{ item.deviceName || '未知设备' }}</b><small>{{ item.deviceId }}</small></td>
            <td>{{ metricLabel(item) }}<small><code>{{ item.name }}</code></small></td>
            <td><StatusPill :status="riskStatus(item) || 'unknown'" /></td>
            <td><b>{{ formatMetricValue(item.value, item.unit) }}</b></td>
            <td>{{ ago(item.collectedAt) }}</td>
            <td>{{ storageRiskAdvice(item) }}</td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有达到阈值的存储风险</b><small>这不代表未上报能力为健康，请同时检查能力状态。</small></div></div>
    </section>

    <div class="storage-grid">
      <section v-for="items in groups" :key="items[0]?.deviceId" class="card storage-device-card">
        <div class="section-title compact"><div><h2>{{ items[0]?.deviceName || '未知设备' }}</h2><span class="muted">{{ items.length }} 项存储证据</span></div><StatusPill :status="items.some((item) => riskStatus(item) === 'critical') ? 'critical' : items.some((item) => riskItems.includes(item)) ? 'warning' : 'healthy'" /></div>
        <div class="storage-measure"><span>根文件系统</span><b>{{ display(items, ['filesystem.root.usage', 'btrfs.usage']) }}</b></div>
        <div class="storage-measure"><span>磁盘温度</span><b>{{ display(items, ['disk.temperature'], 0) }}</b></div>
        <div class="storage-measure"><span>NVMe Media Errors</span><b>{{ display(items, ['disk.nvme.media_errors'], 0) }}</b></div>
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
