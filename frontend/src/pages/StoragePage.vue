<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Capability, Metric } from '@/types'
import { ago, bytes, formatMetricValue, metricLabel, storageRiskAdvice, storageRiskStatus } from '@/utils'
import PageState from '@/components/PageState.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Metric[]; updatedAt: string; capabilities: Capability[]; summary: { totalBytes: number; fillWithin30Days: number } }
interface VolumeResource { usage: Metric; mount: string; size: number; free: number; filesystem: string; backingDevice: string; physicalDevice: string }

const checking = ref(false)
const checkMessage = ref('')
const selectedResourceKey = ref('')
const historyHours = ref(336)
const historyMode = ref<'preset' | 'custom'>('preset')
const customFrom = ref('')
const customTo = ref('')
const appliedCustomFrom = ref('')
const appliedCustomTo = ref('')
const historySeries = ref<ChartSeries[]>([])
const historyLoading = ref(false)
const historyError = ref('')
let historyRequest = 0

const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [storage, operations] = await Promise.all([
    api<{ items: Metric[] | null; updatedAt: string; summary?: { totalBytes: number; fillWithin30Days: number } }>('/api/v1/storage'),
    api<{ capabilities: Capability[] | null }>('/api/v1/operations').catch(() => ({ capabilities: null })),
  ])
  return { ...storage, items: storage.items || [], summary: storage.summary || { totalBytes: 0, fillWithin30Days: 0 }, capabilities: operations.capabilities || [] }
})

const itemList = computed(() => data.value?.items || [])
const riskStatus = (item: Metric) => item.risk || storageRiskStatus(item)
const metricTime = (item: Metric) => new Date(item.collectedAt).getTime()
function latestMetric(items: Metric[]) { return [...items].sort((a, b) => metricTime(b) - metricTime(a))[0] }
function metricFor(device: string, name: string) {
  return latestMetric(itemList.value.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device.replace('/dev/', '') && item.name === name))
}
function physicalDeviceFromBacking(backing: string) {
  const name = backing.split('/').pop() || ''
  const nvme = name.match(/^(nvme\d+n\d+)p\d+$/)
  if (nvme) return nvme[1]
  const sd = name.match(/^(sd[a-z]+)\d+$/)
  return sd?.[1] || name
}
function volumeName(mount: string) {
  if (mount === '/lzcsys/data') return '主数据卷'
  if (mount === '/lzcsys/var') return '系统卷'
  if (mount.startsWith('/lzcsys/run/mnt/')) return '备份卷'
  if (mount.startsWith('/lzcsys/storage/')) return '扩展数据卷'
  return mount
}
function diskPurpose(device: string, diskVolumes: VolumeResource[]) {
  if (diskVolumes.some((volume) => volume.mount === '/lzcsys/var')) return '系统盘'
  if (diskVolumes.some((volume) => volume.mount === '/lzcsys/data')) return '主数据盘'
  if (diskVolumes.some((volume) => volume.mount.startsWith('/lzcsys/run/mnt/'))) return '备份盘'
  if (diskVolumes.some((volume) => volume.mount.startsWith('/lzcsys/storage/'))) return '扩展数据盘'
  return device.startsWith('nvme') ? '固态磁盘' : '未挂载磁盘'
}

const volumes = computed<VolumeResource[]>(() => {
  const btrfsUsage = itemList.value.filter((item) => item.name === 'btrfs.usage')
  const usageItems = btrfsUsage.length ? btrfsUsage : itemList.value.filter((item) => item.name === 'filesystem.root.usage')
  const latestByMount = new Map<string, Metric>()
  for (const usage of usageItems) {
    const mount = usage.labels?.mount || '未知卷'
    const current = latestByMount.get(mount)
    if (!current || (!current.labels?.backing_device && usage.labels?.backing_device) || metricTime(usage) > metricTime(current)) latestByMount.set(mount, usage)
  }
  return [...latestByMount.values()].map((usage) => {
    const mount = usage.labels?.mount || '未知卷'
    const atMount = (name: string) => latestMetric(itemList.value.filter((item) => item.name === name && item.labels?.mount === mount))
    const size = atMount('btrfs.size')
    const free = latestMetric(itemList.value.filter((item) => (item.name === 'btrfs.free_estimated' || item.name === 'filesystem.root.available') && item.labels?.mount === mount))
    const backingDevice = usage.labels?.backing_device || size?.labels?.backing_device || ''
    return {
      usage,
      mount,
      size: size?.value || (free?.value && usage.value < 100 ? free.value / (1 - usage.value / 100) : 0),
      free: free?.value || 0,
      filesystem: usage.name.startsWith('btrfs.') ? 'Btrfs' : '文件系统',
      backingDevice,
      physicalDevice: physicalDeviceFromBacking(backingDevice),
    }
  }).sort((a, b) => b.usage.value - a.usage.value)
})

const physicalDisks = computed(() => {
  const inventory = itemList.value.filter((item) => item.name === 'disk.capacity')
  const devices = inventory.length ? inventory : itemList.value.filter((item) => item.name.startsWith('disk.') && !String(item.labels?.device || '').startsWith('dm-'))
  const unique = new Map<string, Metric>()
  for (const item of devices) {
    const device = String(item.labels?.device || '').replace('/dev/', '')
    const current = unique.get(device)
    if (device && (!current || (!current.labels?.serial && item.labels?.serial) || metricTime(item) > metricTime(current))) unique.set(device, item)
  }
  return [...unique.entries()].map(([device, base]) => {
    const identity = latestMetric(itemList.value.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.serial))
      || latestMetric(itemList.value.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.model))
    const temperature = metricFor(device, 'disk.temperature')
    const hours = metricFor(device, 'disk.power_on_hours')
    const risks = itemList.value.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device && riskStatus(item))
    const diskVolumes = volumes.value.filter((volume) => volume.physicalDevice === device)
    const status = risks.some((item) => riskStatus(item) === 'critical') ? 'critical' : risks.length ? 'warning' : 'healthy'
    return { device, base, model: identity?.labels?.model || base.labels?.model, serial: identity?.labels?.serial || base.labels?.serial, temperature, hours, risks, volumes: diskVolumes, purpose: diskPurpose(device, diskVolumes), status }
  }).sort((a, b) => Number(b.status === 'critical') - Number(a.status === 'critical') || Number(b.status === 'warning') - Number(a.status === 'warning') || b.base.value - a.base.value)
})

const btrfsVolumes = computed(() => volumes.value.filter((item) => item.filesystem === 'Btrfs').map((volume) => {
  const atMount = (name: string) => latestMetric(itemList.value.filter((item) => item.name === name && item.labels?.mount === volume.mount))
  const errors = ['btrfs.write_io_errors', 'btrfs.read_io_errors', 'btrfs.flush_io_errors', 'btrfs.corruption_errors', 'btrfs.generation_errors'].reduce((sum, name) => sum + (atMount(name)?.value || 0), 0)
  const missing = atMount('btrfs.device_missing')?.value || 0
  const scrubKnown = atMount('btrfs.scrub.known')?.value === 1
  return { ...volume, allocated: atMount('btrfs.allocated')?.value || 0, unallocated: atMount('btrfs.unallocated')?.value || 0, errors, missing, scrubKnown, status: errors || missing ? 'critical' : volume.usage.value >= 90 ? 'warning' : 'healthy' }
}))

const riskItems = computed(() => {
  const latest = new Map<string, Metric>()
  for (const item of itemList.value.filter((metric) => riskStatus(metric))) {
    const key = [item.deviceId, item.name, item.labels?.device || '', item.labels?.mount || ''].join('|')
    const current = latest.get(key)
    if (!current || metricTime(item) > metricTime(current)) latest.set(key, item)
  }
  return [...latest.values()].sort((a, b) => Number(riskStatus(a) === 'warning') - Number(riskStatus(b) === 'warning') || b.value - a.value)
})
const capabilityStatus = (name: string) => data.value?.capabilities.find((item) => item.capability.includes(name))
const selectedVolume = computed(() => selectedResourceKey.value.startsWith('volume:') ? volumes.value.find((volume) => `volume:${volume.mount}` === selectedResourceKey.value) : undefined)
const selectedDisk = computed(() => {
  if (selectedResourceKey.value.startsWith('disk:')) return physicalDisks.value.find((disk) => `disk:${disk.device}` === selectedResourceKey.value)
  return physicalDisks.value.find((disk) => disk.device === selectedVolume.value?.physicalDevice)
})
const historyTitle = computed(() => selectedVolume.value ? `使用趋势 · ${volumeName(selectedVolume.value.mount)}` : selectedDisk.value ? `I/O 趋势 · ${selectedDisk.value.device}` : '历史趋势')
const historyUnit = computed(() => selectedVolume.value ? '%' : ' MiB/s')

watch([physicalDisks, volumes], () => {
  const valid = physicalDisks.value.some((disk) => `disk:${disk.device}` === selectedResourceKey.value)
    || volumes.value.some((volume) => `volume:${volume.mount}` === selectedResourceKey.value)
  if (!valid) {
    const preferred = volumes.value[0]
    selectedResourceKey.value = preferred ? `volume:${preferred.mount}` : physicalDisks.value[0] ? `disk:${physicalDisks.value[0].device}` : ''
  }
}, { immediate: true })
watch([selectedResourceKey, historyHours], () => { if (historyMode.value === 'preset') loadHistory() })

function historyRange() {
  return historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
    ? `from=${encodeURIComponent(appliedCustomFrom.value)}&to=${encodeURIComponent(appliedCustomTo.value)}`
    : `hours=${historyHours.value}`
}
function chartPoint(item: Metric) {
  const date = new Date(item.collectedAt)
  return { value: item.value, at: date.toLocaleString('zh-CN'), label: date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }) }
}
function counterRates(items: Metric[], device: string) {
  const points = items.filter((item) => String(item.labels?.device || '').replace('/dev/', '') === device).sort((a, b) => metricTime(a) - metricTime(b))
  return points.slice(1).map((item, index) => {
    const previous = points[index]
    const seconds = Math.max(1, (metricTime(item) - metricTime(previous)) / 1000)
    return { ...chartPoint(item), value: Math.max(0, item.value - previous.value) / seconds / 1024 / 1024 }
  })
}
async function loadHistory() {
  const volume = selectedVolume.value
  const disk = selectedDisk.value
  const deviceId = volume?.usage.deviceId || disk?.base.deviceId
  if (!deviceId || (!volume && !disk)) { historySeries.value = []; return }
  const request = ++historyRequest
  historyLoading.value = true
  historyError.value = ''
  try {
    if (volume) {
      const result = await api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=${encodeURIComponent(volume.usage.name)}&${historyRange()}`)
      if (request === historyRequest) historySeries.value = [{ name: volumeName(volume.mount), color: '#2563eb', points: (result.items || []).filter((item) => item.labels?.mount === volume.mount).map(chartPoint) }]
    } else if (disk) {
      const [read, write] = await Promise.all([
        api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=disk.io.read.bytes_total&${historyRange()}`),
        api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=disk.io.write.bytes_total&${historyRange()}`),
      ])
      if (request === historyRequest) historySeries.value = [
        { name: '读取', color: '#2563eb', points: counterRates(read.items || [], disk.device) },
        { name: '写入', color: '#10b981', points: counterRates(write.items || [], disk.device) },
      ]
    }
  } catch (reason) {
    if (request === historyRequest) { historySeries.value = []; historyError.value = reason instanceof Error ? reason.message : String(reason) }
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}
function selectDisk(device: string) { selectedResourceKey.value = `disk:${device}` }
function selectVolume(mount: string) { selectedResourceKey.value = `volume:${mount}` }
function setPreset(hours: number) { historyMode.value = 'preset'; historyHours.value = hours; loadHistory() }
function showCustomRange() {
  historyMode.value = 'custom'
  if (!customTo.value) {
    const now = new Date()
    const from = new Date(now.getTime() - 7 * 24 * 3600 * 1000)
    customTo.value = toLocalInput(now)
    customFrom.value = toLocalInput(from)
  }
}
function toLocalInput(date: Date) {
  const offset = date.getTimezoneOffset() * 60000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}
function applyCustomRange() {
  const from = new Date(customFrom.value)
  const to = new Date(customTo.value)
  if (!customFrom.value || !customTo.value || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from >= to) { historyError.value = '请选择有效的开始和结束时间'; return }
  if (to.getTime() - from.getTime() > 30 * 24 * 3600 * 1000) { historyError.value = '单次查询范围不能超过 30 天'; return }
  appliedCustomFrom.value = from.toISOString(); appliedCustomTo.value = to.toISOString(); historyError.value = ''; loadHistory()
}
async function runStorageCheck() {
  checking.value = true; checkMessage.value = ''
  try {
    const result = await api<{ points: number; warnings: string[] }>('/api/v1/storage/check', { method: 'POST' })
    checkMessage.value = `只读检查完成：更新 ${result.points} 项指标${result.warnings?.length ? `，${result.warnings.length} 项受限` : ''}`
    await refresh(); await loadHistory()
  } catch (reason) { checkMessage.value = reason instanceof Error ? reason.message : String(reason) }
  finally { checking.value = false }
}
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="等待物理磁盘、文件系统与 SMART 指标。" @retry="refresh">
    <div class="page-intro"><div><h2>存储与 Btrfs</h2><p>从物理磁盘进入卷、容量趋势和 Btrfs 健康状态。</p></div><div class="intro-actions"><span class="muted">更新 {{ ago(data?.updatedAt) }}</span><button class="secondary-button" :disabled="checking" @click="runStorageCheck">{{ checking ? '检查中…' : '立即只读检查' }}</button></div></div>
    <p v-if="checkMessage" class="operation-evidence" role="status">{{ checkMessage }}</p>
    <div class="stats four">
      <StatCard label="物理磁盘" :value="physicalDisks.length" hint="不包含 dm 加密映射设备" />
      <StatCard label="存储卷" :value="volumes.length" hint="已关联到对应物理磁盘" />
      <StatCard label="磁盘风险" :value="riskItems.length" hint="SMART、容量与 Btrfs" :tone="riskItems.length ? 'amber' : 'green'" />
      <StatCard label="Btrfs" :value="btrfsVolumes.length" :hint="btrfsVolumes.some((x) => x.errors || x.missing) ? '存在设备错误或缺失空间' : '未发现设备错误或缺失空间'" />
    </div>

    <section class="card storage-resource-card">
      <div class="section-title"><div><h2>存储资源</h2><span class="muted">物理磁盘、下属卷和历史趋势在同一视图中联动</span></div></div>
      <div class="storage-resource-layout">
        <div class="storage-resource-list">
          <article v-for="disk in physicalDisks" :key="disk.device" class="storage-disk-node" :class="{ active: selectedDisk?.device === disk.device }">
            <button class="storage-disk-select" type="button" @click="selectDisk(disk.device)">
              <span class="storage-device-icon">{{ disk.base.labels?.media === 'ssd' ? 'SSD' : 'HDD' }}</span>
              <span><b>{{ disk.device }} · {{ disk.purpose }}</b><small>{{ disk.model || '型号待采集' }} · {{ bytes(disk.base.value) }}</small><small>{{ disk.serial || '序列号未知' }}</small></span>
              <StatusPill :status="disk.status" />
            </button>
            <div v-if="disk.volumes.length" class="storage-volume-branches">
              <button v-for="volume in disk.volumes" :key="volume.mount" type="button" :class="{ active: selectedResourceKey === `volume:${volume.mount}` }" @click="selectVolume(volume.mount)">
                <span><b>{{ volumeName(volume.mount) }}</b><small>{{ volume.filesystem }} · {{ volume.mount }}</small></span>
                <span class="volume-usage"><b>{{ volume.usage.value.toFixed(1) }}%</b><i><em :style="{ width: `${Math.min(100, volume.usage.value)}%` }" /></i></span>
              </button>
            </div>
            <p v-else class="storage-no-volume">尚未发现已挂载存储卷</p>
          </article>
          <div v-if="!physicalDisks.length" class="inline-empty">尚未获得物理磁盘清单。</div>
        </div>

        <div class="storage-resource-detail">
          <template v-if="selectedVolume">
            <div class="storage-detail-heading"><div><small>{{ selectedDisk?.device || selectedVolume.usage.deviceName || '未知设备' }} · {{ selectedDisk?.purpose || selectedVolume.filesystem }}</small><h3>{{ volumeName(selectedVolume.mount) }}</h3><p>{{ selectedVolume.mount }}</p></div><StatusPill :status="selectedVolume.usage.value >= 90 ? 'warning' : 'healthy'" /></div>
            <div class="storage-detail-stats"><span><small>总容量</small><b>{{ bytes(selectedVolume.size) }}</b></span><span><small>已使用</small><b>{{ bytes(Math.max(0, selectedVolume.size - selectedVolume.free)) }}</b></span><span><small>预计可用</small><b>{{ bytes(selectedVolume.free) }}</b></span><span><small>当前使用率</small><b>{{ selectedVolume.usage.value.toFixed(1) }}%</b></span></div>
          </template>
          <template v-else-if="selectedDisk">
            <div class="storage-detail-heading"><div><small>{{ selectedDisk.purpose }}</small><h3>{{ selectedDisk.device }} · {{ selectedDisk.model }}</h3><p>{{ selectedDisk.serial }}</p></div><StatusPill :status="selectedDisk.status" /></div>
            <div class="storage-detail-stats"><span><small>容量</small><b>{{ bytes(selectedDisk.base.value) }}</b></span><span><small>介质 / 接口</small><b>{{ (selectedDisk.base.labels?.media || '未知').toUpperCase() }} / {{ (selectedDisk.base.labels?.transport || '未知').toUpperCase() }}</b></span><span><small>温度</small><b>{{ selectedDisk.temperature ? formatMetricValue(selectedDisk.temperature.value, selectedDisk.temperature.unit, 0) : '未知' }}</b></span><span><small>通电时间</small><b>{{ selectedDisk.hours ? formatMetricValue(selectedDisk.hours.value, selectedDisk.hours.unit, 0) : '未知' }}</b></span></div>
          </template>

          <div class="storage-history-heading"><div><h3>{{ historyTitle }}</h3><span class="muted">{{ selectedVolume ? '文件系统整体使用率' : '根据磁盘累计读写量计算平均速率' }}</span></div><div class="range-tabs"><button v-for="option in [{ h: 24, l: '24小时' }, { h: 168, l: '7天' }, { h: 336, l: '14天' }, { h: 720, l: '30天' }]" :key="option.h" :class="{ active: historyMode === 'preset' && historyHours === option.h }" @click="setPreset(option.h)">{{ option.l }}</button><button :class="{ active: historyMode === 'custom' }" @click="showCustomRange">自定义</button></div></div>
          <div v-if="historyMode === 'custom'" class="storage-custom-range"><label>开始<input v-model="customFrom" type="datetime-local" /></label><label>结束<input v-model="customTo" type="datetime-local" /></label><button class="secondary-button" @click="applyCustomRange">应用</button></div>
          <p v-if="historyError" class="operation-evidence warning">{{ historyError }}</p>
          <div v-if="historyLoading" class="inline-empty">正在读取历史数据…</div>
          <LineChart v-else :series="historySeries" :min="0" :max="selectedVolume ? 100 : undefined" :unit="historyUnit" :height="250" />
        </div>
      </div>
    </section>

    <section class="card btrfs-health-card"><div class="section-title"><div><h2>Btrfs 健康中心</h2><span class="muted">整体空间、已分配空间、设备错误和 Scrub 分开判断</span></div><StatusPill :status="capabilityStatus('btrfs')?.status || 'unknown'" /></div>
      <div class="btrfs-grid"><article v-for="volume in btrfsVolumes" :key="volume.mount" class="btrfs-volume"><div><b>{{ volumeName(volume.mount) }}</b><StatusPill :status="volume.status" /></div><small class="muted">{{ volume.mount }}</small><dl><span><dt>整体使用率</dt><dd>{{ volume.usage.value.toFixed(1) }}%</dd></span><span><dt>预计可用</dt><dd>{{ bytes(volume.free) }}</dd></span><span><dt>已分配</dt><dd>{{ bytes(volume.allocated) }}</dd></span><span><dt>未分配</dt><dd>{{ bytes(volume.unallocated) }}</dd></span><span><dt>设备错误</dt><dd>{{ volume.errors }}</dd></span><span><dt>缺失设备空间</dt><dd>{{ volume.missing ? bytes(volume.missing) : '0 B' }}</dd></span></dl><p :class="volume.scrubKnown ? 'muted' : 'operation-evidence warning'">{{ volume.scrubKnown ? '已有 Scrub 状态记录' : '尚无 Scrub 历史，不能判定最近校验时间' }}</p></article><div v-if="!btrfsVolumes.length" class="inline-empty">Btrfs 只读采集尚未返回；点击“立即只读检查”。</div></div>
    </section>

    <section class="card storage-risk-card"><div class="section-title"><div><h2>需要处理的存储风险</h2><span class="muted">只列出达到规则阈值的证据</span></div></div><div v-if="riskItems.length" class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>资源</th><th>风险</th><th>当前值</th><th>采集时间</th><th>建议</th></tr></thead><tbody><tr v-for="item in riskItems" :key="`${item.deviceId}-${item.name}-${metricLabel(item)}`"><td>{{ item.deviceName || '未知设备' }}</td><td>{{ metricLabel(item) }}<small><code>{{ item.name }}</code></small></td><td><StatusPill :status="riskStatus(item) || 'unknown'" /></td><td><b>{{ formatMetricValue(item.value, item.unit) }}</b></td><td>{{ ago(item.collectedAt) }}</td><td>{{ storageRiskAdvice(item) }}</td></tr></tbody></table></div><div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有达到阈值的存储风险</b></div></div></section>

    <section class="card capability-card"><div class="section-title"><div><h2>存储采集能力</h2></div></div><div class="capability-grid"><div v-for="name in ['filesystem','btrfs','smart','nvme']" :key="name"><span>{{ name.toUpperCase() }}</span><StatusPill :status="capabilityStatus(name)?.status || 'unknown'" /><small>{{ capabilityStatus(name)?.detail || '当前 API 未返回此能力状态' }}</small></div></div></section>
  </PageState>
</template>
