<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling } from '@/composables'
import type { Capability, Metric } from '@/types'
import { ago, bytes, dateTime, formatMetricValue, metricLabel, monthDay, parseBeijingDateTimeInput, storageRiskAdvice, storageRiskStatus, toBeijingDateTimeInput } from '@/utils'
import PageState from '@/components/PageState.vue'
import AppPagination from '@/components/AppPagination.vue'
import LineChart, { type ChartSeries } from '@/components/LineChart.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Metric[]; updatedAt: string; capabilities: Capability[]; summary: { totalBytes: number; fillWithin30Days: number } }
interface VolumeResource {
  key: string
  deviceId: string
  usage: Metric
  mount: string
  size: number
  free: number
  filesystem: string
  backingDevice: string
  physicalDevice: string
}

const checking = ref(false)
const checkMessage = ref('')
const historyHours = ref(336)
const historyMode = ref<'preset' | 'custom'>('preset')
const customFrom = ref('')
const customTo = ref('')
const appliedCustomFrom = ref('')
const appliedCustomTo = ref('')
const diskHistory = ref<Record<string, ChartSeries[]>>({})
const volumeHistory = ref<Record<string, ChartSeries[]>>({})
const historyLoading = ref(false)
const historyError = ref('')
let historyRequest = 0
const deepLink = (() => {
  const query = location.hash.split('?')[1] || ''
  const params = new URLSearchParams(query)
  return {
    deviceId: params.get('deviceId') || '',
    disk: (params.get('disk') || '').replace(/^\/dev\//, ''),
  }
})()
const highlightedDiskKey = ref('')
const locatedDiskLabel = ref('')

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
function metricFor(deviceId: string, device: string, name: string) {
  return latestMetric(itemList.value.filter((item) =>
    item.deviceId === deviceId
    && String(item.labels?.device || '').replace('/dev/', '') === device.replace('/dev/', '')
    && item.name === name))
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
function diskBrand(model?: string, vendor?: string) {
  if (vendor?.trim()) return vendor.trim()
  const value = String(model || '').trim()
  const upper = value.toUpperCase()
  if (upper.startsWith('TOSHIBA')) return 'Toshiba'
  if (upper.startsWith('WDC ') || upper.startsWith('WD_') || upper.startsWith('WD ')) return 'Western Digital'
  if (/^ST\d/.test(upper)) return 'Seagate'
  if (upper.startsWith('LEXAR')) return 'Lexar'
  if (upper.startsWith('SAMSUNG')) return 'Samsung'
  if (upper.startsWith('KINGSTON')) return 'Kingston'
  if (upper.startsWith('CRUCIAL')) return 'Crucial'
  if (upper.startsWith('MICRON')) return 'Micron'
  if (upper.startsWith('INTEL')) return 'Intel'
  return '品牌未知'
}

const volumes = computed<VolumeResource[]>(() => {
  const btrfsUsage = itemList.value.filter((item) => item.name === 'btrfs.usage')
  const usageItems = btrfsUsage.length ? btrfsUsage : itemList.value.filter((item) => item.name === 'filesystem.root.usage')
  const latestByMount = new Map<string, Metric>()
  for (const usage of usageItems) {
    const mount = usage.labels?.mount || '未知卷'
    const key = `${usage.deviceId}\u0000${mount}`
    const current = latestByMount.get(key)
    if (!current || (!current.labels?.backing_device && usage.labels?.backing_device) || metricTime(usage) > metricTime(current)) latestByMount.set(key, usage)
  }
  return [...latestByMount.values()].map((usage) => {
    const mount = usage.labels?.mount || '未知卷'
    const deviceId = usage.deviceId || ''
    const key = `${deviceId}\u0000${mount}`
    const atMount = (name: string) => latestMetric(itemList.value.filter((item) =>
      item.deviceId === deviceId && item.name === name && item.labels?.mount === mount))
    const size = atMount('btrfs.size')
    const free = latestMetric(itemList.value.filter((item) =>
      item.deviceId === deviceId
      && (item.name === 'btrfs.free_estimated' || item.name === 'filesystem.root.available')
      && item.labels?.mount === mount))
    const backingDevice = usage.labels?.backing_device || size?.labels?.backing_device || ''
    return {
      key,
      deviceId,
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
    const key = `${item.deviceId}\u0000${device}`
    const current = unique.get(key)
    if (device && (!current || (!current.labels?.serial && item.labels?.serial) || metricTime(item) > metricTime(current))) unique.set(key, item)
  }
  return [...unique.entries()].map(([key, base]) => {
    const deviceId = base.deviceId || ''
    const device = String(base.labels?.device || '').replace('/dev/', '')
    const identity = latestMetric(itemList.value.filter((item) => item.deviceId === deviceId && String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.serial))
      || latestMetric(itemList.value.filter((item) => item.deviceId === deviceId && String(item.labels?.device || '').replace('/dev/', '') === device && item.labels?.model))
    const temperature = metricFor(deviceId, device, 'disk.temperature')
    const hours = metricFor(deviceId, device, 'disk.power_on_hours')
    const risks = itemList.value.filter((item) => item.deviceId === deviceId && String(item.labels?.device || '').replace('/dev/', '') === device && riskStatus(item))
    const diskVolumes = volumes.value.filter((volume) => volume.deviceId === deviceId && volume.physicalDevice === device)
    const status = risks.some((item) => riskStatus(item) === 'critical') ? 'critical' : risks.length ? 'warning' : 'healthy'
    const model = identity?.labels?.model || base.labels?.model
    return {
      key, deviceId, device, base, model,
      brand: diskBrand(model, identity?.labels?.vendor || base.labels?.vendor),
      serial: identity?.labels?.serial || base.labels?.serial,
      temperature, hours, risks, volumes: diskVolumes,
      purpose: diskPurpose(device, diskVolumes), status,
    }
  }).sort((a, b) => Number(b.status === 'critical') - Number(a.status === 'critical') || Number(b.status === 'warning') - Number(a.status === 'warning') || b.base.value - a.base.value)
})
const orphanVolumes = computed(() => volumes.value.filter((volume) =>
  !physicalDisks.value.some((disk) => disk.deviceId === volume.deviceId && disk.device === volume.physicalDevice)))

function storageDiskID(key: string) {
  return `storage-disk-${encodeURIComponent(key).replaceAll('%', '_')}`
}
watch(physicalDisks, async (disks) => {
  if (!deepLink.deviceId || !deepLink.disk) return
  const target = disks.find((disk) => disk.deviceId === deepLink.deviceId && disk.device === deepLink.disk)
  if (!target) return
  highlightedDiskKey.value = target.key
  locatedDiskLabel.value = `${target.base.deviceName || target.deviceId} · ${target.device}`
  await nextTick()
  document.getElementById(storageDiskID(target.key))?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}, { immediate: true })

const btrfsVolumes = computed(() => volumes.value.filter((item) => item.filesystem === 'Btrfs').map((volume) => {
  const atMount = (name: string) => latestMetric(itemList.value.filter((item) =>
    item.deviceId === volume.deviceId && item.name === name && item.labels?.mount === volume.mount))
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
const btrfsPagination = usePagination(btrfsVolumes, 10)
const riskPagination = usePagination(riskItems, 20)
const capabilityStatus = (name: string) => data.value?.capabilities.find((item) => item.capability.includes(name))
watch(() => data.value?.updatedAt, () => { if (data.value?.updatedAt) loadAllHistory() })

function historyRange() {
  return historyMode.value === 'custom' && appliedCustomFrom.value && appliedCustomTo.value
    ? `from=${encodeURIComponent(appliedCustomFrom.value)}&to=${encodeURIComponent(appliedCustomTo.value)}`
    : `hours=${historyHours.value}`
}
function chartPoint(item: Metric) {
  return { value: item.value, at: dateTime(item.collectedAt), label: monthDay(item.collectedAt) }
}
function counterRates(items: Metric[], deviceId: string, device: string) {
  const points = items.filter((item) =>
    (!item.deviceId || item.deviceId === deviceId)
    && String(item.labels?.device || '').replace('/dev/', '') === device).sort((a, b) => metricTime(a) - metricTime(b))
  return points.slice(1).map((item, index) => {
    const previous = points[index]
    const seconds = Math.max(1, (metricTime(item) - metricTime(previous)) / 1000)
    return { ...chartPoint(item), value: Math.max(0, item.value - previous.value) / seconds / 1024 / 1024 }
  })
}
async function loadAllHistory() {
  const groups = new Map<string, { disks: typeof physicalDisks.value; volumes: VolumeResource[] }>()
  for (const disk of physicalDisks.value) {
    const deviceId = disk.base.deviceId
    if (!deviceId) continue
    const group = groups.get(deviceId) || { disks: [], volumes: [] }
    group.disks.push(disk)
    groups.set(deviceId, group)
  }
  for (const volume of volumes.value) {
    const deviceId = volume.usage.deviceId
    if (!deviceId) continue
    const group = groups.get(deviceId) || { disks: [], volumes: [] }
    group.volumes.push(volume)
    groups.set(deviceId, group)
  }
  if (!groups.size) { diskHistory.value = {}; volumeHistory.value = {}; return }
  const request = ++historyRequest
  historyLoading.value = true
  historyError.value = ''
  try {
    const nextDisks: Record<string, ChartSeries[]> = {}
    const nextVolumes: Record<string, ChartSeries[]> = {}
    await Promise.all([...groups.entries()].map(async ([deviceId, group]) => {
      const volumeNames = [...new Set(group.volumes.map((volume) => volume.usage.name))]
      const emptyHistory = Promise.resolve<{ items: Metric[] }>({ items: [] })
      const [read, write, ...volumeResults] = await Promise.all([
        group.disks.length ? api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=disk.io.read.bytes_total&${historyRange()}`) : emptyHistory,
        group.disks.length ? api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=disk.io.write.bytes_total&${historyRange()}`) : emptyHistory,
        ...volumeNames.map((name) => api<{ items: Metric[] }>(`/api/v1/devices/${encodeURIComponent(deviceId)}/metrics?name=${encodeURIComponent(name)}&${historyRange()}`)),
      ])
      for (const disk of group.disks) {
        nextDisks[disk.key] = [
          { name: '读取', color: '#2563eb', points: counterRates(read.items || [], deviceId, disk.device) },
          { name: '写入', color: '#10b981', points: counterRates(write.items || [], deviceId, disk.device) },
        ]
      }
      const byName = new Map(volumeNames.map((name, index) => [name, volumeResults[index]?.items || []]))
      for (const volume of group.volumes) {
        nextVolumes[volume.key] = [{ name: volumeName(volume.mount), color: '#2563eb', points: (byName.get(volume.usage.name) || []).filter((item) => (!item.deviceId || item.deviceId === deviceId) && item.labels?.mount === volume.mount).map(chartPoint) }]
      }
    }))
    if (request === historyRequest) { diskHistory.value = nextDisks; volumeHistory.value = nextVolumes }
  } catch (reason) {
    if (request === historyRequest) { diskHistory.value = {}; volumeHistory.value = {}; historyError.value = reason instanceof Error ? reason.message : String(reason) }
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}
function setPreset(hours: number) { historyMode.value = 'preset'; historyHours.value = hours; loadAllHistory() }
function showCustomRange() {
  historyMode.value = 'custom'
  if (!customTo.value) {
    const now = new Date()
    const from = new Date(now.getTime() - 7 * 24 * 3600 * 1000)
    customTo.value = toBeijingDateTimeInput(now)
    customFrom.value = toBeijingDateTimeInput(from)
  }
}
function applyCustomRange() {
  const from = parseBeijingDateTimeInput(customFrom.value)
  const to = parseBeijingDateTimeInput(customTo.value)
  if (!customFrom.value || !customTo.value || !Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from >= to) { historyError.value = '请选择有效的开始和结束时间'; return }
  if (to.getTime() - from.getTime() > 30 * 24 * 3600 * 1000) { historyError.value = '单次查询范围不能超过 30 天'; return }
  appliedCustomFrom.value = from.toISOString(); appliedCustomTo.value = to.toISOString(); historyError.value = ''; loadAllHistory()
}
async function runStorageCheck() {
  checking.value = true; checkMessage.value = ''
  try {
    const result = await api<{ points: number; warnings: string[] }>('/api/v1/storage/check', { method: 'POST' })
    checkMessage.value = `只读检查完成：更新 ${result.points} 项指标${result.warnings?.length ? `，${result.warnings.length} 项受限` : ''}`
    await refresh(); await loadAllHistory()
  } catch (reason) { checkMessage.value = reason instanceof Error ? reason.message : String(reason) }
  finally { checking.value = false }
}
</script>

<template>
  <PageState :loading="loading" :error="error" :empty="data?.items.length === 0" empty-title="尚无存储数据" empty-text="等待物理磁盘、文件系统与 SMART 指标。" @retry="refresh">
    <div class="page-intro"><div><h2>存储与 Btrfs</h2></div><div class="intro-actions"><span class="muted">更新 {{ ago(data?.updatedAt) }}</span><button class="secondary-button" :disabled="checking" @click="runStorageCheck">{{ checking ? '检查中…' : '立即只读检查' }}</button></div></div>
    <p v-if="locatedDiskLabel" class="storage-location-evidence" role="status"><b>已定位到 {{ locatedDiskLabel }}</b><span>以下高亮卡片为告警对应的物理磁盘。</span></p>
    <p v-if="checkMessage" class="operation-evidence" role="status">{{ checkMessage }}</p>
    <div class="stats four">
      <StatCard label="物理磁盘" :value="physicalDisks.length" hint="不包含 dm 加密映射设备" />
      <StatCard label="存储卷" :value="volumes.length" hint="已关联到对应物理磁盘" />
      <StatCard label="磁盘风险" :value="riskItems.length" hint="SMART、容量与 Btrfs" :tone="riskItems.length ? 'amber' : 'green'" />
      <StatCard label="Btrfs" :value="btrfsVolumes.length" :hint="btrfsVolumes.some((x) => x.errors || x.missing) ? '存在设备错误或缺失空间' : '未发现设备错误或缺失空间'" />
    </div>

    <section class="card storage-resource-card">
      <div class="section-title storage-expanded-title"><div><h2>存储资源</h2></div><div class="range-tabs"><button v-for="option in [{ h: 24, l: '24小时' }, { h: 168, l: '7天' }, { h: 336, l: '14天' }, { h: 720, l: '30天' }]" :key="option.h" :class="{ active: historyMode === 'preset' && historyHours === option.h }" @click="setPreset(option.h)">{{ option.l }}</button><button :class="{ active: historyMode === 'custom' }" @click="showCustomRange">自定义</button></div></div>
      <div v-if="historyMode === 'custom'" class="storage-custom-range"><label>开始（北京时间）<input v-model="customFrom" type="datetime-local" /></label><label>结束（北京时间）<input v-model="customTo" type="datetime-local" /></label><button class="secondary-button" @click="applyCustomRange">应用</button></div>
      <p v-if="historyError" class="operation-evidence warning">{{ historyError }}</p>
      <div v-if="historyLoading" class="inline-empty">正在读取全部磁盘与卷的历史数据…</div>
      <div class="storage-expanded-list">
        <article v-for="disk in physicalDisks" :id="storageDiskID(disk.key)" :key="disk.key" class="storage-expanded-disk" :class="{ targeted: highlightedDiskKey === disk.key }">
          <div class="storage-expanded-disk-heading">
            <div class="storage-expanded-identity"><span class="storage-device-icon">{{ disk.base.labels?.media === 'ssd' ? 'SSD' : 'HDD' }}</span><div><small>{{ disk.base.deviceName || '未知设备' }} · {{ disk.purpose }}</small><h3>{{ disk.device }} · {{ disk.brand }}</h3><p>{{ disk.model || '型号待采集' }} · {{ disk.serial || '序列号未知' }}</p></div></div>
            <StatusPill :status="disk.status" />
          </div>
          <div class="storage-detail-stats"><span><small>容量</small><b>{{ bytes(disk.base.value) }}</b></span><span><small>介质 / 接口</small><b>{{ (disk.base.labels?.media || '未知').toUpperCase() }} / {{ (disk.base.labels?.transport || '未知').toUpperCase() }}</b></span><span><small>温度</small><b>{{ disk.temperature ? formatMetricValue(disk.temperature.value, disk.temperature.unit, 0) : '未知' }}</b></span><span><small>通电时间</small><b>{{ disk.hours ? formatMetricValue(disk.hours.value, disk.hours.unit, 0) : '未知' }}</b></span></div>
          <div class="storage-expanded-charts" :class="{ single: !disk.volumes.length }">
            <section class="storage-history-panel"><div><h4>磁盘 I/O 趋势</h4><span class="muted">读取 / 写入平均速率</span></div><LineChart :series="diskHistory[disk.key] || []" :min="0" unit=" MiB/s" :height="190" /></section>
            <section v-for="volume in disk.volumes" :key="volume.key" class="storage-history-panel volume-panel">
              <div class="storage-volume-heading"><div><h4>{{ volumeName(volume.mount) }} · 使用趋势</h4><span class="muted">{{ volume.mount }}</span></div><StatusPill :status="volume.usage.value >= 90 ? 'warning' : 'healthy'" /></div>
              <div class="storage-volume-summary"><span>使用率 <b>{{ volume.usage.value.toFixed(1) }}%</b></span><span>已用 <b>{{ bytes(Math.max(0, volume.size - volume.free)) }}</b></span><span>可用 <b>{{ bytes(volume.free) }}</b></span><span>总容量 <b>{{ bytes(volume.size) }}</b></span></div>
              <LineChart :series="volumeHistory[volume.key] || []" :min="0" :max="100" unit="%" :height="190" />
            </section>
          </div>
          <p v-if="!disk.volumes.length" class="storage-unmounted-note">该磁盘当前没有已挂载卷，仍展示物理磁盘 I/O 历史。</p>
        </article>
        <article v-for="volume in orphanVolumes" :key="`orphan-${volume.key}`" class="storage-expanded-disk orphan-volume-card">
          <div class="storage-expanded-disk-heading"><div class="storage-expanded-identity"><span class="storage-device-icon">VOL</span><div><small>{{ volume.usage.deviceName || '未知设备' }} · 尚未关联物理磁盘</small><h3>{{ volumeName(volume.mount) }}</h3><p>{{ volume.mount }}</p></div></div><StatusPill :status="volume.usage.value >= 90 ? 'warning' : 'healthy'" /></div>
          <div class="storage-volume-summary orphan-summary"><span>使用率 <b>{{ volume.usage.value.toFixed(1) }}%</b></span><span>已用 <b>{{ bytes(Math.max(0, volume.size - volume.free)) }}</b></span><span>可用 <b>{{ bytes(volume.free) }}</b></span><span>总容量 <b>{{ bytes(volume.size) }}</b></span></div>
          <section class="storage-history-panel volume-panel"><div><h4>存储卷使用趋势</h4><span class="muted">等待底层设备身份后将自动归入对应物理磁盘</span></div><LineChart :series="volumeHistory[volume.key] || []" :min="0" :max="100" unit="%" :height="190" /></section>
        </article>
        <div v-if="!physicalDisks.length && !orphanVolumes.length" class="inline-empty">尚未获得物理磁盘清单。</div>
      </div>
    </section>

    <section class="card btrfs-health-card"><div class="section-title"><div><h2>Btrfs 健康中心</h2></div><StatusPill :status="capabilityStatus('btrfs')?.status || 'unknown'" /></div>
      <div class="btrfs-grid"><article v-for="volume in btrfsPagination.pagedItems.value" :key="volume.key" class="btrfs-volume"><div><b>{{ volumeName(volume.mount) }}</b><StatusPill :status="volume.status" /></div><small class="muted">{{ volume.usage.deviceName || '未知设备' }} · {{ volume.mount }}</small><dl><span><dt>整体使用率</dt><dd>{{ volume.usage.value.toFixed(1) }}%</dd></span><span><dt>预计可用</dt><dd>{{ bytes(volume.free) }}</dd></span><span><dt>已分配</dt><dd>{{ bytes(volume.allocated) }}</dd></span><span><dt>未分配</dt><dd>{{ bytes(volume.unallocated) }}</dd></span><span><dt>设备错误</dt><dd>{{ volume.errors }}</dd></span><span><dt>缺失设备空间</dt><dd>{{ volume.missing ? bytes(volume.missing) : '0 B' }}</dd></span></dl><p :class="volume.scrubKnown ? 'muted' : 'operation-evidence warning'">{{ volume.scrubKnown ? '已有 Scrub 状态记录' : '尚无 Scrub 历史，不能判定最近校验时间' }}</p></article><div v-if="!btrfsVolumes.length" class="inline-empty">Btrfs 只读采集尚未返回；点击“立即只读检查”。</div></div>
      <AppPagination v-model:page="btrfsPagination.page.value" v-model:page-size="btrfsPagination.pageSize.value" :total="btrfsPagination.total.value" :page-count="btrfsPagination.pageCount.value" :range-start="btrfsPagination.rangeStart.value" :range-end="btrfsPagination.rangeEnd.value" label="Btrfs 卷分页" />
    </section>

    <section class="card storage-risk-card"><div class="section-title"><div><h2>需要处理的存储风险</h2></div></div><div v-if="riskItems.length" class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>资源</th><th>风险</th><th>当前值</th><th>采集时间</th><th>建议</th></tr></thead><tbody><tr v-for="item in riskPagination.pagedItems.value" :key="`${item.deviceId}-${item.name}-${metricLabel(item)}`"><td>{{ item.deviceName || '未知设备' }}</td><td>{{ metricLabel(item) }}<small><code>{{ item.name }}</code></small></td><td><StatusPill :status="riskStatus(item) || 'unknown'" /></td><td><b>{{ formatMetricValue(item.value, item.unit) }}</b></td><td>{{ ago(item.collectedAt) }}</td><td>{{ storageRiskAdvice(item) }}</td></tr></tbody></table></div><div v-else class="healthy-empty horizontal"><span>✓</span><div><b>当前没有达到阈值的存储风险</b></div></div><AppPagination v-model:page="riskPagination.page.value" v-model:page-size="riskPagination.pageSize.value" :total="riskPagination.total.value" :page-count="riskPagination.pageCount.value" :range-start="riskPagination.rangeStart.value" :range-end="riskPagination.rangeEnd.value" label="存储风险分页" /></section>

    <section class="card capability-card"><div class="section-title"><div><h2>存储采集能力</h2></div></div><div class="capability-grid"><div v-for="name in ['filesystem','btrfs','smart','nvme']" :key="name"><span>{{ name.toUpperCase() }}</span><StatusPill :status="capabilityStatus(name)?.status || 'unknown'" /><small>{{ capabilityStatus(name)?.detail || '当前 API 未返回此能力状态' }}</small></div></div></section>
  </PageState>
</template>
