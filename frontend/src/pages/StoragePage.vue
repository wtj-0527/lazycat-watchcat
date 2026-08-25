<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Capability, Metric } from '@/types'
import { ago, bytes, formatMetricValue, metricLabel, storageRiskAdvice, storageRiskStatus } from '@/utils'
import PageState from '@/components/PageState.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Metric[]; updatedAt: string; capabilities: Capability[]; summary: { totalBytes: number; fillWithin30Days: number }; history: Metric[]; historyTarget?: Metric }
const checking = ref(false)
const checkMessage = ref('')
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [storage, operations] = await Promise.all([
    api<{ items: Metric[] | null; updatedAt: string; summary?: { totalBytes: number; fillWithin30Days: number } }>('/api/v1/storage'),
    api<{ capabilities: Capability[] | null }>('/api/v1/operations').catch(() => ({ capabilities: null })),
  ])
  const items = storage.items || []
  const historyTarget = [...items].filter((item) => item.name === 'filesystem.root.usage' || item.name === 'btrfs.usage').sort((a, b) => b.value - a.value)[0]
  const history = historyTarget?.deviceId ? await api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(historyTarget.deviceId)}/metrics?name=${encodeURIComponent(historyTarget.name)}&hours=336`).then((result) => result.items || []).catch(() => []) : []
  return { ...storage, items, history, historyTarget, summary: storage.summary || { totalBytes: 0, fillWithin30Days: 0 }, capabilities: operations.capabilities || [] }
})
const itemList = computed(() => data.value?.items || [])
const metricFor = (device: string, name: string) => itemList.value.find((item) => item.labels?.device === device && item.name === name)
const physicalDisks = computed(() => {
  const inventory = itemList.value.filter((item) => item.name === 'disk.capacity')
  const devices = inventory.length ? inventory : itemList.value.filter((item) => item.name.startsWith('disk.') && !String(item.labels?.device || '').startsWith('dm-'))
  const unique = new Map<string, Metric>()
  for (const item of devices) {
    const device = String(item.labels?.device || '').replace('/dev/', '')
    if (device && !unique.has(device)) unique.set(device, item)
  }
  return [...unique.entries()].map(([device, base]) => {
    const identity = itemList.value.find((item) => String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.serial)
      || itemList.value.find((item) => String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.model)
    const temperature = metricFor(device, 'disk.temperature') || metricFor(`/dev/${device}`, 'disk.temperature')
    const hours = metricFor(device, 'disk.power_on_hours') || metricFor(`/dev/${device}`, 'disk.power_on_hours')
    const risks = itemList.value.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device && riskStatus(item))
    return { device, base, model: identity?.labels?.model || base.labels?.model, serial: identity?.labels?.serial || base.labels?.serial, temperature, hours, risks, status: risks.some((x) => riskStatus(x) === 'critical') ? 'critical' : risks.length ? 'warning' : 'healthy' }
  }).sort((a, b) => b.base.value - a.base.value)
})
const volumes = computed(() => {
  const btrfsUsage = itemList.value.filter((item) => item.name === 'btrfs.usage')
  const usageItems = btrfsUsage.length ? btrfsUsage : itemList.value.filter((item) => item.name === 'filesystem.root.usage')
  return usageItems.map((usage) => {
  const mount = usage.labels?.mount || '未知卷'
  const size = itemList.value.find((item) => item.name === 'btrfs.size' && item.labels?.mount === mount)
  const free = itemList.value.find((item) => (item.name === 'btrfs.free_estimated' || item.name === 'filesystem.root.available') && item.labels?.mount === mount)
  return { usage, mount, size: size?.value || (free?.value && usage.value < 100 ? free.value / (1 - usage.value / 100) : 0), free: free?.value || 0, filesystem: usage.name.startsWith('btrfs.') ? 'Btrfs' : '文件系统' }
  }).sort((a, b) => b.usage.value - a.usage.value)
})
const btrfsVolumes = computed(() => volumes.value.filter((item) => item.filesystem === 'Btrfs').map((volume) => {
  const atMount = (name: string) => itemList.value.find((item) => item.name === name && item.labels?.mount === volume.mount)
  const errors = ['btrfs.write_io_errors','btrfs.read_io_errors','btrfs.flush_io_errors','btrfs.corruption_errors','btrfs.generation_errors'].reduce((sum, name) => sum + (atMount(name)?.value || 0), 0)
  const missing = atMount('btrfs.device_missing')?.value || 0
  const scrubKnown = atMount('btrfs.scrub.known')?.value === 1
  return { ...volume, allocated: atMount('btrfs.allocated')?.value || 0, unallocated: atMount('btrfs.unallocated')?.value || 0, errors, missing, scrubKnown, status: errors || missing ? 'critical' : volume.usage.value >= 90 ? 'warning' : 'healthy' }
}))
const riskStatus = (item: Metric) => item.risk || storageRiskStatus(item)
const riskItems = computed(() => {
  const latest = new Map<string, Metric>()
  for (const item of itemList.value.filter((metric) => riskStatus(metric))) {
    const key = [item.deviceId, item.name, item.labels?.device || '', item.labels?.mount || ''].join('|')
    const current = latest.get(key)
    if (!current || new Date(item.collectedAt).getTime() > new Date(current.collectedAt).getTime()) latest.set(key, item)
  }
  return [...latest.values()].sort((a, b) => Number(riskStatus(a) === 'warning') - Number(riskStatus(b) === 'warning') || b.value - a.value)
})
const critical = computed(() => riskItems.value.filter((item) => riskStatus(item) === 'critical').length)
const capabilityStatus = (name: string) => data.value?.capabilities.find((item) => item.capability.includes(name))
const capacityTrend = computed<ChartSeries[]>(() => {
  const target = data.value?.historyTarget
  const points = (data.value?.history || []).filter((point) => !target || (point.labels?.mount || '') === (target.labels?.mount || ''))
  return points.length ? [{ name: target?.labels?.mount || '存储卷', color: '#2563eb', points: points.map((point) => ({ value: point.value, at: new Date(point.collectedAt).toLocaleString('zh-CN'), label: new Date(point.collectedAt).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }) })) }] : []
})
async function runStorageCheck() {
  checking.value = true; checkMessage.value = ''
  try { const result = await api<{ points: number; warnings: string[] }>('/api/v1/storage/check', { method: 'POST' }); checkMessage.value = `只读检查完成：更新 ${result.points} 项指标${result.warnings?.length ? `，${result.warnings.length} 项受限` : ''}`; await refresh() }
  catch (reason) { checkMessage.value = reason instanceof Error ? reason.message : String(reason) }
  finally { checking.value = false }
}
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="等待物理磁盘、文件系统与 SMART 指标。" @retry="refresh">
    <div class="page-intro"><div><h2>存储与 Btrfs</h2><p>物理磁盘、存储卷、Btrfs 文件系统和风险证据分层展示。</p></div><div class="intro-actions"><span class="muted">更新 {{ ago(data?.updatedAt) }}</span><button class="secondary-button" :disabled="checking" @click="runStorageCheck">{{ checking ? '检查中…' : '立即只读检查' }}</button></div></div>
    <p v-if="checkMessage" class="operation-evidence" role="status">{{ checkMessage }}</p>
    <div class="stats four">
      <StatCard label="物理磁盘" :value="physicalDisks.length" hint="不包含 dm 加密映射设备" />
      <StatCard label="存储卷" :value="volumes.length" hint="按文件系统挂载去重" />
      <StatCard label="磁盘风险" :value="riskItems.length" hint="SMART、容量与 Btrfs" :tone="riskItems.length ? 'amber' : 'green'" />
      <StatCard label="Btrfs" :value="btrfsVolumes.length" :hint="btrfsVolumes.some((x) => x.errors || x.missing) ? '存在设备错误或缺失空间' : '未发现设备错误或缺失空间'" />
    </div>

    <section class="card storage-inventory-card">
      <div class="section-title"><div><h2>物理磁盘</h2><span class="muted">型号、介质、容量、温度与 SMART 风险</span></div></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>型号</th><th>介质/接口</th><th>容量</th><th>温度</th><th>通电</th><th>状态</th></tr></thead><tbody>
        <tr v-for="disk in physicalDisks" :key="disk.device"><td class="device"><b>{{ disk.device }}</b><small>{{ disk.serial || '序列号未知' }}</small></td><td>{{ disk.model || '型号待采集' }}</td><td>{{ (disk.base.labels?.media || '未知').toUpperCase() }} · {{ (disk.base.labels?.transport || '未知').toUpperCase() }}</td><td><b>{{ disk.base.name === 'disk.capacity' ? bytes(disk.base.value) : '待采集' }}</b></td><td>{{ disk.temperature ? formatMetricValue(disk.temperature.value, disk.temperature.unit, 0) : '未知' }}</td><td>{{ disk.hours ? formatMetricValue(disk.hours.value, disk.hours.unit, 0) : '未知' }}</td><td><StatusPill :status="disk.status" /></td></tr>
      </tbody></table></div>
    </section>

    <div class="chart-panel-grid">
      <section class="card"><div class="section-title"><div><h2>最高使用率卷 · 14 天趋势</h2><span class="muted">按文件系统整体使用率，不使用 Btrfs 已分配块占比冒充磁盘占用</span></div></div><LineChart :series="capacityTrend" :min="0" :max="100" unit="%" :height="230" /></section>
      <section class="card"><div class="section-title"><div><h2>存储卷</h2><span class="muted">整体已用、可用空间与文件系统类型</span></div></div><div class="volume-list"><div v-for="volume in volumes" :key="volume.mount" class="volume-row"><div><b>{{ volume.usage.deviceName || '未知设备' }} · {{ volume.mount }}</b><small>{{ volume.filesystem }} · 可用 {{ bytes(volume.free) }}</small></div><strong>{{ volume.usage.value.toFixed(1) }}%</strong><i><em :style="{ width: `${Math.min(100, volume.usage.value)}%` }" /></i><span>{{ bytes(volume.size) }}</span></div><div v-if="!volumes.length" class="inline-empty">尚未获得卷容量。</div></div></section>
    </div>

    <section class="card btrfs-health-card"><div class="section-title"><div><h2>Btrfs 健康中心</h2><span class="muted">整体空间、已分配空间、设备错误和 Scrub 分开判断</span></div><StatusPill :status="capabilityStatus('btrfs')?.status || 'unknown'" /></div>
      <div class="btrfs-grid"><article v-for="volume in btrfsVolumes" :key="volume.mount" class="btrfs-volume"><div><b>{{ volume.mount }}</b><StatusPill :status="volume.status" /></div><dl><span><dt>整体使用率</dt><dd>{{ volume.usage.value.toFixed(1) }}%</dd></span><span><dt>预计可用</dt><dd>{{ bytes(volume.free) }}</dd></span><span><dt>已分配</dt><dd>{{ bytes(volume.allocated) }}</dd></span><span><dt>未分配</dt><dd>{{ bytes(volume.unallocated) }}</dd></span><span><dt>设备错误</dt><dd>{{ volume.errors }}</dd></span><span><dt>缺失设备空间</dt><dd>{{ volume.missing ? bytes(volume.missing) : '0 B' }}</dd></span></dl><p :class="volume.scrubKnown ? 'muted' : 'operation-evidence warning'">{{ volume.scrubKnown ? '已有 Scrub 状态记录' : '尚无 Scrub 历史，不能判定最近校验时间' }}</p></article><div v-if="!btrfsVolumes.length" class="inline-empty">Btrfs 只读采集尚未返回；点击“立即只读检查”。</div></div>
    </section>

    <section class="card storage-risk-card"><div class="section-title"><div><h2>需要处理的存储风险</h2><span class="muted">只列出达到规则阈值的证据</span></div></div><div v-if="riskItems.length" class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>资源</th><th>风险</th><th>当前值</th><th>采集时间</th><th>建议</th></tr></thead><tbody><tr v-for="item in riskItems" :key="`${item.deviceId}-${item.name}-${metricLabel(item)}`"><td>{{ item.deviceName || '未知设备' }}</td><td>{{ metricLabel(item) }}<small><code>{{ item.name }}</code></small></td><td><StatusPill :status="riskStatus(item) || 'unknown'" /></td><td><b>{{ formatMetricValue(item.value, item.unit) }}</b></td><td>{{ ago(item.collectedAt) }}</td><td>{{ storageRiskAdvice(item) }}</td></tr></tbody></table></div><div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有达到阈值的存储风险</b></div></div></section>

    <section class="card capability-card"><div class="section-title"><div><h2>存储采集能力</h2></div></div><div class="capability-grid"><div v-for="name in ['filesystem','btrfs','smart','nvme']" :key="name"><span>{{ name.toUpperCase() }}</span><StatusPill :status="capabilityStatus(name)?.status || 'unknown'" /><small>{{ capabilityStatus(name)?.detail || '当前 API 未返回此能力状态' }}</small></div></div></section>
  </PageState>
</template>
