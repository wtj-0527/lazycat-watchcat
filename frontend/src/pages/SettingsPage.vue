<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling, useRovingTabs } from '@/composables'
import type { Backup, Capability, Device, Stability } from '@/types'
import { ago, backupType, bytes, dateTime, duration, parseBeijingDateTimeInput } from '@/utils'
import PageState from '@/components/PageState.vue'
import AppPagination from '@/components/AppPagination.vue'
import StatusPill from '@/components/StatusPill.vue'
import { appConfirm } from '@/dialog'

interface Settings {
  appVersion: string; deploymentMode: string; embeddedCollector: boolean; singleUser: boolean; maxDevices: number
  collectIntervalSeconds: number; advancedIntervalSeconds: number; rawRetentionDays: number; rollupRetentionDays: number
  auditRetentionDays: number; inspectionRetentionDays: number; backupRetentionCount: number
  notificationChannel: string; notificationDelivery: string
  dailyInspectionHour: number; weeklyInspectionHour: number
}
interface Operations { capabilities: Capability[]; schedule: { daily: { hour: number }; weekly: { hour: number }; timezone: string } }
interface DatabaseStatus { databaseSize: number; integrityOk: boolean; integrityError?: string; backupCount: number; latestBackup?: Backup }
interface AlertRule { metric: string; label: string; warning: number; critical: number; enabled: boolean }
interface MaintenanceWindow { id: string; name: string; startsAt: string; endsAt: string; enabled: boolean }
interface AuditEntry { id: number; action: string; subjectType: string; subjectId: string; metadata: Record<string, unknown>; createdAt: string }
interface UnusedImage { id: string; tags: string[]; size: number; createdAt?: string; category: 'dangling' | 'cached' }
interface UnusedImages {
  available: boolean; count: number; totalSize: number
  danglingCount: number; danglingSize: number; cachedCount: number; cachedSize: number
  items: UnusedImage[]; error?: string
}
interface ImagePruneResult { imagesDeleted: number; referencesUntagged: number; spaceReclaimed: number }
interface ImageDeleteResult { imageId: string; referencesUntagged: number; deleteRecords: number }
interface Payload {
  settings: Settings; operations: Operations; database: DatabaseStatus; backups: Backup[]; stability: Stability
  devices: Device[]; rules: AlertRule[]; windows: MaintenanceWindow[]; audit: AuditEntry[]; unusedImages: UnusedImages; upstream: UpstreamStatus
}
interface PairingCode { code: string; expiresAt: string }
interface UpstreamStatus { paired: boolean; hubUrl?: string; deviceId?: string; lastSuccessAt?: string; lastError?: string }
interface RestoreResult { status: string; backup: string; message: string }
interface OperationEvidence { status: 'success' | 'warning' | 'error'; message: string }
type Tab = 'onboarding' | 'groups' | 'capabilities' | 'thresholds' | 'notifications' | 'maintenance' | 'retention' | 'audit'
const tabs: Array<[Tab, string]> = [
  ['groups', '设备组与标签'], ['capabilities', 'Collector 能力'], ['thresholds', '告警阈值'],
  ['notifications', '通知渠道'], ['maintenance', '维护窗口'], ['retention', '数据保留'], ['audit', '用户与审计'],
]
const props = defineProps<{ initialTab?: Tab }>()
const isOnboardingRoute = computed(() => props.initialTab === 'onboarding')
const emit = defineEmits<{ toast: [message: string] }>()
const { selected: tab, select: selectTab, move: moveTab } = useRovingTabs(tabs, props.initialTab || 'thresholds', 'settings-tab-')
const pairing = ref<PairingCode>()
const pairingLoading = ref(false)
const collectorHubURL = ref(localStorage.getItem('watchcatInviteEndpoint') || window.location.origin)
const showConnectionSettings = ref(false)
const connectMode = ref<'invite' | 'join'>('invite')
const joinInvitation = ref('')
const joinLoading = ref(false)
const backupLoading = ref(false)
const backupEvidence = ref<OperationEvidence>()
const restoreEvidence = ref<OperationEvidence>()
const stabilityLoading = ref(false)
const stabilityEvidence = ref<OperationEvidence>()
const imageCleanupLoading = ref(false)
const deletingImageId = ref('')
const imageCleanupEvidence = ref<OperationEvidence>()
const settingsEvidence = ref<OperationEvidence>()
const maintenanceName = ref('')
const maintenanceStart = ref('')
const maintenanceEnd = ref('')
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [settings, operations, database, backups, stability, devices, rules, windows, audit, unusedImages, upstream] = await Promise.all([
    api<Settings>('/api/v1/settings'), api<Operations>('/api/v1/operations'), api<DatabaseStatus>('/api/v1/database/status'),
    api<{ items: Backup[] }>('/api/v1/backups'), api<Stability>('/api/v1/stability'),
    api<{ items: Device[] }>('/api/v1/devices').catch(() => ({ items: [] })),
    api<{ items: AlertRule[] }>('/api/v1/alert-rules').catch(() => ({ items: [] })),
    api<{ items: MaintenanceWindow[] }>('/api/v1/maintenance-windows').catch(() => ({ items: [] })),
    api<{ items: AuditEntry[] }>('/api/v1/audit?limit=100').catch(() => ({ items: [] })),
    api<UnusedImages>('/api/v1/docker/images/unused').catch((reason) => ({
      available: false, count: 0, totalSize: 0, danglingCount: 0, danglingSize: 0, cachedCount: 0, cachedSize: 0, items: [],
      error: reason instanceof Error ? reason.message : String(reason),
    })),
    api<UpstreamStatus>('/api/v1/upstream').catch(() => ({ paired: false })),
  ])
  return {
    settings,
    operations: { ...operations, capabilities: operations.capabilities || [] },
    database,
    backups: backups.items || [],
    stability,
    devices: devices.items || [],
    rules: rules.items || [],
    windows: windows.items || [],
    audit: audit.items || [],
    unusedImages: { ...unusedImages, items: unusedImages.items || [] },
    upstream,
  }
})
const localCapability = computed(() => data.value?.operations.capabilities.filter((item) => !item.capability.startsWith('remote.')) || [])
const connectedDevices = computed(() => (data.value?.devices || []).filter((device) => (
  !device.local && !device.capabilities?.includes('collector.embedded')
)))
const danglingImages = computed(() => data.value?.unusedImages.items.filter((item) => item.category === 'dangling') || [])
const cachedImages = computed(() => data.value?.unusedImages.items.filter((item) => item.category === 'cached') || [])
const settingsDevices = computed(() => data.value?.devices || [])
const settingsRules = computed(() => data.value?.rules || [])
const maintenanceWindows = computed(() => data.value?.windows || [])
const backupItems = computed(() => data.value?.backups || [])
const auditItems = computed(() => data.value?.audit || [])
const connectedDevicePagination = usePagination(connectedDevices, 10)
const settingsDevicePagination = usePagination(settingsDevices, 20)
const capabilityPagination = usePagination(localCapability, 20)
const rulePagination = usePagination(settingsRules, 20)
const maintenancePagination = usePagination(maintenanceWindows, 10)
const backupPagination = usePagination(backupItems, 10)
const danglingPagination = usePagination(danglingImages, 10)
const cachedPagination = usePagination(cachedImages, 10)
const auditPagination = usePagination(auditItems, 20)

async function createPairingCode() {
  pairingLoading.value = true
  try {
    pairing.value = await api<PairingCode>('/api/v1/pairing-codes', { method: 'POST' })
    emit('toast', '一次性配对码已生成')
  } catch (reason) { emit('toast', reason instanceof Error ? reason.message : String(reason)) }
  finally { pairingLoading.value = false }
}
async function copyPairingCode() {
  if (!pairing.value) return
  await navigator.clipboard.writeText(pairing.value.code)
  emit('toast', '配对码已复制')
}
function buildPairingLink() {
  if (!pairing.value) throw new Error('pairing code required')
  const hub = new URL(collectorHubURL.value.trim())
  if (hub.protocol !== 'http:' && hub.protocol !== 'https:') throw new Error('invalid protocol')
  hub.search = ''
  hub.hash = new URLSearchParams({ 'pairing-code': pairing.value.code }).toString()
  return hub.toString()
}
async function copyPairingLink() {
  try {
    await navigator.clipboard.writeText(buildPairingLink())
    localStorage.setItem('watchcatInviteEndpoint', collectorHubURL.value.trim())
    emit('toast', '设备邀请已复制，可直接粘贴到目标设备')
  } catch {
    emit('toast', '请先生成邀请并填写目标设备可访问的有效地址')
  }
}
async function joinExistingWatchCat() {
  if (!joinInvitation.value.trim()) {
    emit('toast', '请先粘贴完整的设备邀请')
    return
  }
  joinLoading.value = true
  try {
    await api<UpstreamStatus>('/api/v1/upstream/join', {
      method: 'POST', body: JSON.stringify({ invitation: joinInvitation.value.trim() }),
    })
    joinInvitation.value = ''
    await refresh()
    emit('toast', '已加入现有 WatchCat，正在上报第一批指标')
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    joinLoading.value = false
  }
}
async function disconnectUpstream() {
  if (!await appConfirm({ title: '断开并彻底移除', message: '主 WatchCat 将永久删除本设备的历史指标、告警、运行状态和凭据；本机也会删除上游配置。重新加入必须生成新邀请。', confirmText: '彻底移除', danger: true })) return
  await api('/api/v1/upstream', { method: 'DELETE' })
  await refresh()
  emit('toast', '已从主 WatchCat 彻底移除，并清除本机上游凭据')
}
async function saveDeviceMetadata(device: Device) {
  await api(`/api/v1/devices/${encodeURIComponent(device.id)}/metadata`, {
    method: 'PUT', body: JSON.stringify({ group: device.group || '', location: device.location || '', labels: device.labels || {} }),
  })
  settingsEvidence.value = { status: 'success', message: `${device.name} 的设备资料已保存` }
  await refresh()
}
function updateLabels(device: Device, event: Event) {
  const value = (event.target as HTMLInputElement).value
  device.labels = Object.fromEntries(value.split(',').map((item) => item.trim()).filter(Boolean).map((item) => {
    const [key, ...rest] = item.split('=')
    return [key, rest.join('=')]
  }))
}
async function saveRules() {
  if (!data.value) return
  await api('/api/v1/alert-rules', { method: 'PUT', body: JSON.stringify({ items: data.value.rules }) })
  settingsEvidence.value = { status: 'success', message: '告警阈值已保存并立即重新评估' }
  await refresh()
}
async function addMaintenanceWindow() {
  if (!maintenanceName.value || !maintenanceStart.value || !maintenanceEnd.value) return
  const startsAt = parseBeijingDateTimeInput(maintenanceStart.value)
  const endsAt = parseBeijingDateTimeInput(maintenanceEnd.value)
  if (Number.isNaN(startsAt.getTime()) || Number.isNaN(endsAt.getTime()) || startsAt >= endsAt) {
    settingsEvidence.value = { status: 'error', message: '请选择有效的北京时间范围，且结束时间必须晚于开始时间' }
    return
  }
  await api('/api/v1/maintenance-windows', {
    method: 'POST',
    body: JSON.stringify({ name: maintenanceName.value, startsAt: startsAt.toISOString(), endsAt: endsAt.toISOString(), enabled: true }),
  })
  maintenanceName.value = maintenanceStart.value = maintenanceEnd.value = ''
  settingsEvidence.value = { status: 'success', message: '维护窗口已创建；窗口内告警通知将自动抑制' }
  await refresh()
}
async function deleteMaintenanceWindow(id: string) {
  await api(`/api/v1/maintenance-windows/${encodeURIComponent(id)}`, { method: 'DELETE' })
  await refresh()
}
async function sendTestNotification() {
  await api('/api/v1/notifications/test', { method: 'POST' })
  settingsEvidence.value = { status: 'success', message: '测试通知已进入持久投递队列' }
  emit('toast', '测试通知已排队')
}
async function saveOperationalSettings() {
  if (!data.value) return
  const settings = data.value.settings
  await api('/api/v1/settings', {
    method: 'PUT',
    body: JSON.stringify({
      rawRetentionDays: settings.rawRetentionDays,
      rollupRetentionDays: settings.rollupRetentionDays,
      auditRetentionDays: settings.auditRetentionDays,
      inspectionRetentionDays: settings.inspectionRetentionDays,
      backupRetentionCount: settings.backupRetentionCount,
      dailyInspectionHour: settings.dailyInspectionHour,
      weeklyInspectionHour: settings.weeklyInspectionHour,
    }),
  })
  settingsEvidence.value = { status: 'success', message: '巡检计划与数据保留设置已保存并立即生效' }
  await refresh()
}
async function createBackup() {
  backupLoading.value = true
  backupEvidence.value = undefined
  try {
    const created = await api<Backup>('/api/v1/backups', { method: 'POST' })
    const refreshed = await refresh()
    const verified = refreshed?.backups.find((item) => item.name === created.name)
    if (created.verified && verified?.verified && verified.sha256 === created.sha256) {
      backupEvidence.value = { status: 'success', message: `已回读验证 ${created.name} · SHA-256 ${created.sha256.slice(0, 16)}…` }
      emit('toast', '在线备份已创建并回读验证')
    } else {
      backupEvidence.value = { status: 'warning', message: `备份 ${created.name} 已创建，但列表回读未确认校验结果` }
      emit('toast', '备份已创建，回读验证尚未确认')
    }
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    backupEvidence.value = { status: 'error', message }
    emit('toast', message)
  } finally {
    backupLoading.value = false
  }
}
async function deleteBackup(name: string) {
  if (!await appConfirm({ title: '删除数据库备份', message: `确定删除备份 ${name}？删除后无法恢复。`, confirmText: '删除备份', danger: true })) return
  try {
    await api(`/api/v1/backups/${encodeURIComponent(name)}`, { method: 'DELETE' })
    backupEvidence.value = { status: 'success', message: `备份 ${name} 已删除` }
    emit('toast', '备份已删除')
    await refresh()
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    backupEvidence.value = { status: 'error', message }
    emit('toast', message)
  }
}
async function restoreBackup(name: string) {
  if (!await appConfirm({ title: '恢复数据库备份', message: '恢复将重启 WatchCat 并造成短暂断连；替换前会再创建安全备份。', confirmText: '确认恢复', danger: true })) return
  restoreEvidence.value = undefined
  try {
    const result = await api<RestoreResult>(`/api/v1/backups/${encodeURIComponent(name)}/restore`, { method: 'POST' })
    if (result.status !== 'restart-scheduled' || result.backup !== name) throw new Error('恢复请求响应与目标备份不一致')
    restoreEvidence.value = { status: 'success', message: `${result.backup} 已通过校验并排队；数据库将在重启阶段原子替换` }
    emit('toast', '恢复请求已校验并排队，应用即将重启')
    window.setTimeout(() => location.reload(), 4_000)
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    restoreEvidence.value = { status: 'error', message }
    emit('toast', message)
  }
}
async function resetStability() {
  if (!await appConfirm({ title: '重置稳定性观测', message: '确定清零当前稳定性观测并重新计算 7 天周期？', confirmText: '重新开始', danger: true })) return
  stabilityLoading.value = true
  stabilityEvidence.value = undefined
  try {
    const reset = await api<Stability>('/api/v1/stability/reset', { method: 'POST' })
    const refreshed = await refresh()
    if (refreshed?.stability.startedAt === reset.startedAt) {
      stabilityEvidence.value = { status: 'success', message: `已回读确认新观测周期：${dateTime(reset.startedAt)} 开始，当前 ${reset.sampleCount} 次采样` }
      emit('toast', '7 天稳定性观测已重置并回读确认')
    } else {
      stabilityEvidence.value = { status: 'warning', message: `重置 API 已返回新周期 ${dateTime(reset.startedAt)}，但状态回读尚未一致` }
      emit('toast', '稳定性观测已重置，回读尚未确认')
    }
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    stabilityEvidence.value = { status: 'error', message }
    emit('toast', message)
  } finally {
    stabilityLoading.value = false
  }
}
async function pruneUnusedImages() {
  if (!data.value?.unusedImages.available || data.value.unusedImages.danglingCount === 0) return
  const preview = data.value.unusedImages
  if (!await appConfirm({
    title: '清理悬空旧镜像',
    message: `确定清理 ${preview.danglingCount} 个悬空旧镜像？镜像逻辑大小约 ${bytes(preview.danglingSize)}。带标签、可能供暂停应用未来启动使用的缓存镜像不会批量删除。`,
    confirmText: '开始清理',
    danger: true,
  })) return
  imageCleanupLoading.value = true
  imageCleanupEvidence.value = undefined
  try {
    const result = await api<ImagePruneResult>('/api/v1/docker/images/prune', { method: 'POST' })
    imageCleanupEvidence.value = {
      status: 'success',
      message: `悬空镜像清理完成：删除 ${result.imagesDeleted} 个镜像，释放 ${bytes(result.spaceReclaimed)}`,
    }
    emit('toast', `已释放 ${bytes(result.spaceReclaimed)}`)
    await refresh()
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    imageCleanupEvidence.value = { status: 'error', message }
    emit('toast', message)
  } finally {
    imageCleanupLoading.value = false
  }
}
async function deleteUnusedImage(image: UnusedImage) {
  const cached = image.category === 'cached'
  const label = image.tags[0] || image.id.slice(0, 19)
  const warning = cached
    ? '该镜像可能供当前未启动的 LPK 使用；删除后下次启动需要重新拉取。'
    : '该镜像没有标签且未被容器引用。'
  if (!await appConfirm({ title: '删除镜像', message: `确定删除镜像 ${label}？${warning}`, confirmText: '删除镜像', danger: true })) return
  deletingImageId.value = image.id
  imageCleanupEvidence.value = undefined
  try {
    const result = await api<ImageDeleteResult>(`/api/v1/docker/images/${encodeURIComponent(image.id)}`, { method: 'DELETE' })
    imageCleanupEvidence.value = {
      status: 'success',
      message: `镜像 ${result.imageId.slice(0, 19)} 已删除，移除 ${result.deleteRecords} 条镜像记录`,
    }
    emit('toast', '镜像已删除')
    await refresh()
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : String(reason)
    imageCleanupEvidence.value = { status: 'error', message }
    emit('toast', message)
  } finally {
    deletingImageId.value = ''
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro">
      <div><h2>{{ isOnboardingRoute ? '接入新设备' : '设置与治理' }}</h2></div>
    </div>
    <div v-if="!isOnboardingRoute" class="settings-tabs" role="tablist" aria-label="设置分类">
      <button
        v-for="[key, label] in tabs"
        :id="`settings-tab-${key}`"
        :key="key"
        :class="{ active: tab === key }"
        role="tab"
        :aria-selected="tab === key"
        aria-controls="settings-panel"
        :tabindex="tab === key ? 0 : -1"
        @click="selectTab(key)"
        @keydown="moveTab($event, key)"
      >{{ label }}</button>
    </div>

    <div v-if="data" id="settings-panel" role="tabpanel" :aria-label="isOnboardingRoute ? '设备接入' : undefined" :aria-labelledby="isOnboardingRoute ? undefined : `settings-tab-${tab}`">
    <p v-if="settingsEvidence" class="operation-evidence" :class="settingsEvidence.status" role="status">{{ settingsEvidence.message }}</p>

    <template v-if="data && tab === 'onboarding'">
      <div class="device-connect-page">
        <div class="connect-mode-switch" role="tablist" aria-label="设备接入方式">
          <button :class="{ active: connectMode === 'invite' }" role="tab" :aria-selected="connectMode === 'invite'" @click="connectMode = 'invite'">邀请其他设备</button>
          <button :class="{ active: connectMode === 'join' }" role="tab" :aria-selected="connectMode === 'join'" @click="connectMode = 'join'">加入现有 WatchCat</button>
        </div>

        <section v-if="connectMode === 'invite'" class="connect-hero">
          <div class="connect-hero-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24"><path d="M8.5 15.5l7-7M7 17H5.5a4.5 4.5 0 010-9H9m6-1h3.5a4.5 4.5 0 010 9H15" /></svg>
          </div>
          <div class="connect-hero-copy">
            <span class="connect-eyebrow">安全设备接入</span>
            <h1>连接另一台设备</h1>
            <p>生成一个一次性邀请，在目标设备粘贴后即可建立加密连接。无需分别配置设备名称、地址和配对码。</p>
            <div class="connect-hero-meta">
              <span><i />一次性使用</span>
              <span><i />自动过期</span>
              <span><i />独立设备身份</span>
            </div>
          </div>
          <button class="primary-button connect-generate-button" :disabled="pairingLoading" @click="createPairingCode">
            {{ pairingLoading ? '正在生成…' : pairing ? '重新生成邀请' : '生成设备邀请' }}
          </button>
        </section>

        <section v-if="connectMode === 'invite' && pairing" class="invite-ready-card" aria-live="polite">
          <div class="invite-ready-heading">
            <div class="invite-success-icon" aria-hidden="true">✓</div>
            <div>
              <span>邀请已生成</span>
              <h2>复制后发送到目标设备</h2>
            </div>
            <small>有效期至 {{ dateTime(pairing.expiresAt) }}</small>
          </div>
          <div class="invite-code-row">
            <div>
              <span>配对码</span>
              <strong>{{ pairing.code }}</strong>
            </div>
            <button class="primary-button" @click="copyPairingLink">复制设备邀请</button>
          </div>
          <div class="invite-ready-footer">
            <span>邀请内已包含连接地址与配对码</span>
            <div>
              <button class="text-button" @click="copyPairingCode">仅复制配对码</button>
              <button class="text-button" @click="showConnectionSettings = !showConnectionSettings">
                {{ showConnectionSettings ? '收起连接设置' : '修改连接地址' }}
              </button>
            </div>
          </div>
          <div v-if="showConnectionSettings" class="connection-settings-panel">
            <label>
              <span>目标设备可访问的 WatchCat 地址</span>
              <input v-model="collectorHubURL" type="url" placeholder="https://watchcat.example.com">
            </label>
            <p>仅当目标设备无法访问当前地址时修改。可以填写局域网可达地址，或你手动配置的转发地址。</p>
          </div>
        </section>

        <section v-if="connectMode === 'invite'" class="connected-devices-card" aria-live="polite">
          <div class="connected-devices-heading">
            <div>
              <span class="connect-eyebrow">接入结果</span>
              <h2>已接入设备</h2>
              <p>目标设备完成首次上报后会自动出现在这里。</p>
            </div>
            <strong>{{ connectedDevices.length }} 台</strong>
          </div>
          <div v-if="connectedDevices.length" class="connected-device-list">
            <div v-for="device in connectedDevicePagination.pagedItems.value" :key="device.id" class="connected-device-row">
              <span class="connected-device-icon" aria-hidden="true">✓</span>
              <div>
                <b>{{ device.name }}</b>
                <small>{{ device.hostname }} · Collector {{ device.collectorVersion || '未知版本' }}</small>
              </div>
              <div class="connected-device-status">
                <StatusPill :status="device.status || 'unknown'" />
                <small>最近上报 {{ ago(device.lastSeenAt) }}</small>
              </div>
            </div>
            <AppPagination v-model:page="connectedDevicePagination.page.value" v-model:page-size="connectedDevicePagination.pageSize.value" :total="connectedDevicePagination.total.value" :page-count="connectedDevicePagination.pageCount.value" :range-start="connectedDevicePagination.rangeStart.value" :range-end="connectedDevicePagination.rangeEnd.value" label="已接入设备分页" />
          </div>
          <div v-else class="connected-devices-empty">
            <span>尚未接入其他设备</span>
            <small>生成邀请并在目标设备完成加入后，本页会自动刷新。</small>
          </div>
          <a class="secondary-button connected-devices-link" href="#devices">查看全部设备</a>
        </section>

        <section v-if="connectMode === 'join'" class="join-existing-card">
          <div v-if="data.upstream.paired" class="joined-state">
            <div class="invite-success-icon" aria-hidden="true">✓</div>
            <div>
              <span>已加入现有 WatchCat</span>
              <h1>本机正在向主 WatchCat 上报数据</h1>
              <p>{{ data.upstream.hubUrl }}</p>
              <small v-if="data.upstream.lastSuccessAt">最近上报 {{ ago(data.upstream.lastSuccessAt) }}</small>
              <small v-else>连接已建立，正在等待第一批指标上报。</small>
              <em v-if="data.upstream.lastError">{{ data.upstream.lastError }}</em>
            </div>
            <button class="danger-button" @click="disconnectUpstream">双向彻底移除</button>
          </div>
          <template v-else>
            <div class="join-existing-heading">
              <span class="connect-card-icon join" aria-hidden="true">↓</span>
              <div>
                <span class="connect-eyebrow">加入设备群</span>
                <h1>粘贴设备邀请</h1>
                <p>在另一台 WatchCat 中生成邀请，然后将完整内容粘贴到这里。本机名称会自动识别。</p>
              </div>
            </div>
            <label class="join-invitation-field">
              <span>设备邀请</span>
              <textarea v-model="joinInvitation" autocomplete="off" placeholder="粘贴完整邀请，例如：http://WatchCat 地址/#pairing-code=..." />
            </label>
            <div class="join-actions">
              <span>邀请只会使用一次，配对成功后不会保留原始配对码。</span>
              <button class="primary-button" :disabled="joinLoading || !joinInvitation.trim()" @click="joinExistingWatchCat">
                {{ joinLoading ? '正在验证并加入…' : '验证并加入' }}
              </button>
            </div>
          </template>
        </section>

        <div v-if="connectMode === 'invite'" class="connect-guide-grid">
          <section class="connect-guide-card">
            <div class="connect-card-heading">
              <span class="connect-card-icon target" aria-hidden="true">↗</span>
              <div><h2>在目标设备上完成</h2><p>整个过程只需要粘贴一次。</p></div>
            </div>
            <ol class="connect-steps">
              <li><b>1</b><div><strong>打开目标设备上的 WatchCat</strong><span>进入“接入”，选择“加入现有 WatchCat”。</span></div></li>
              <li><b>2</b><div><strong>粘贴设备邀请</strong><span>无需再填写主机名、地址或端口。</span></div></li>
              <li><b>3</b><div><strong>确认加入</strong><span>首次指标上报后，设备会自动出现在列表中。</span></div></li>
            </ol>
          </section>
          <section class="connect-guide-card">
            <div class="connect-card-heading">
              <span class="connect-card-icon secure" aria-hidden="true">✓</span>
              <div><h2>连接受到保护</h2><p>每台设备使用独立身份。</p></div>
            </div>
            <ul class="security-list">
              <li><i>✓</i><div><strong>邀请仅能使用一次</strong><span>使用后立即失效，过期邀请无法再次配对。</span></div></li>
              <li><i>✓</i><div><strong>设备身份独立签发</strong><span>连接建立后使用独立证书上报数据。</span></div></li>
              <li><i>✓</i><div><strong>随时撤销设备</strong><span>从设备列表删除后，需要新邀请才能重新加入。</span></div></li>
            </ul>
          </section>
        </div>
        <div v-else-if="!data.upstream.paired" class="connect-guide-grid join-guide-grid">
          <section class="connect-guide-card">
            <div class="connect-card-heading">
              <span class="connect-card-icon target" aria-hidden="true">↗</span>
              <div><h2>如何获得邀请</h2><p>在作为主节点的 WatchCat 上操作。</p></div>
            </div>
            <ol class="connect-steps">
              <li><b>1</b><div><strong>打开“接入”</strong><span>选择“邀请其他设备”。</span></div></li>
              <li><b>2</b><div><strong>生成设备邀请</strong><span>如有需要，先修改为本机可以访问的连接地址。</span></div></li>
              <li><b>3</b><div><strong>复制并粘贴到这里</strong><span>地址和一次性配对码已经包含在同一条邀请中。</span></div></li>
            </ol>
          </section>
          <section class="connect-guide-card">
            <div class="connect-card-heading">
              <span class="connect-card-icon secure" aria-hidden="true">✓</span>
              <div><h2>加入后会发生什么</h2><p>本机仍可独立使用。</p></div>
            </div>
            <ul class="security-list">
              <li><i>✓</i><div><strong>保留本机监控</strong><span>本机数据库、告警和页面不会被关闭。</span></div></li>
              <li><i>✓</i><div><strong>同步真实指标</strong><span>同一批采集数据会持续上报到主 WatchCat。</span></div></li>
              <li><i>✓</i><div><strong>可随时断开</strong><span>断开只删除本机上游凭据，不会删除本机历史数据。</span></div></li>
            </ul>
          </section>
        </div>
      </div>
    </template>

    <section v-else-if="data && tab === 'groups'" class="card">
      <div class="section-title"><div><h2>设备组与标签</h2></div></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>设备组</th><th>位置</th><th>标签</th><th /></tr></thead><tbody>
        <tr v-for="device in settingsDevicePagination.pagedItems.value" :key="device.id">
          <td><b>{{ device.name }}</b><small>{{ device.hostname }}</small></td>
          <td><input v-model="device.group" aria-label="设备组"></td>
          <td><input v-model="device.location" aria-label="位置"></td>
          <td><input :value="Object.entries(device.labels || {}).map(([key, value]) => `${key}=${value}`).join(', ')" aria-label="标签" @change="updateLabels(device, $event)"></td>
          <td><button class="secondary-button tiny" @click="saveDeviceMetadata(device)">保存</button></td>
        </tr>
      </tbody></table></div>
      <AppPagination v-model:page="settingsDevicePagination.page.value" v-model:page-size="settingsDevicePagination.pageSize.value" :total="settingsDevicePagination.total.value" :page-count="settingsDevicePagination.pageCount.value" :range-start="settingsDevicePagination.rangeStart.value" :range-end="settingsDevicePagination.rangeEnd.value" label="设备组列表分页" />
    </section>

    <section v-else-if="data && tab === 'capabilities'" class="card">
      <div class="section-title"><div><h2>Collector 能力</h2></div></div>
      <div class="capability-list"><div v-for="item in capabilityPagination.pagedItems.value" :key="`${item.capability}-${item.checkedAt}`"><div><b>{{ item.capability }}</b><p>{{ item.detail }}</p><small>检查于 {{ ago(item.checkedAt) }}</small></div><StatusPill :status="item.status || 'unknown'" /></div></div>
      <AppPagination v-model:page="capabilityPagination.page.value" v-model:page-size="capabilityPagination.pageSize.value" :total="capabilityPagination.total.value" :page-count="capabilityPagination.pageCount.value" :range-start="capabilityPagination.rangeStart.value" :range-end="capabilityPagination.rangeEnd.value" label="Collector 能力分页" />
    </section>

    <section v-else-if="data && tab === 'thresholds'" class="card">
      <div class="section-title"><div><h2>告警阈值</h2></div><button class="primary-button" @click="saveRules">保存并重新评估</button></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>规则</th><th>指标</th><th>Warning</th><th>Critical</th><th>启用</th></tr></thead><tbody>
        <tr v-for="rule in rulePagination.pagedItems.value" :key="rule.metric"><td><b>{{ rule.label }}</b></td><td><code>{{ rule.metric }}</code></td><td><input v-model.number="rule.warning" type="number" min="0"></td><td><input v-model.number="rule.critical" type="number" min="0"></td><td><input v-model="rule.enabled" type="checkbox"></td></tr>
      </tbody></table></div>
      <AppPagination v-model:page="rulePagination.page.value" v-model:page-size="rulePagination.pageSize.value" :total="rulePagination.total.value" :page-count="rulePagination.pageCount.value" :range-start="rulePagination.rangeStart.value" :range-end="rulePagination.rangeEnd.value" label="告警阈值分页" />
    </section>

    <section v-else-if="data && tab === 'notifications'" class="card">
      <div class="section-title"><div><h2>通知渠道</h2></div><button class="secondary-button" @click="sendTestNotification">发送测试通知</button></div>
      <div class="settings-grid"><div><span>当前渠道</span><b>{{ data.settings.notificationChannel === 'lazycat' ? 'LazyCat 系统通知' : data.settings.notificationChannel }}</b><StatusPill status="available" /></div><div><span>投递策略</span><b>{{ data.settings.notificationDelivery === 'outbox-retry' ? '持久队列重试' : data.settings.notificationDelivery }}</b><StatusPill status="available" /></div><div><span>待发送</span><b>{{ data.stability.pendingNotifications }}</b><StatusPill :status="data.stability.pendingNotifications ? 'warning' : 'healthy'" /></div></div>
    </section>

    <section v-else-if="data && tab === 'maintenance'" class="card">
      <div class="section-title"><div><h2>维护窗口与巡检计划</h2></div><button class="primary-button" @click="saveOperationalSettings">保存计划</button></div>
      <div class="settings-grid"><label><span>每日巡检小时</span><input v-model.number="data.settings.dailyInspectionHour" type="number" min="0" max="23"></label><label><span>每周日巡检小时</span><input v-model.number="data.settings.weeklyInspectionHour" type="number" min="0" max="23"></label><div><span>时区</span><b>{{ data.operations.schedule.timezone }}</b><StatusPill status="available" /></div></div>
      <div class="maintenance-form"><input v-model="maintenanceName" placeholder="窗口名称"><input v-model="maintenanceStart" type="datetime-local" aria-label="开始时间（北京时间）" title="北京时间"><input v-model="maintenanceEnd" type="datetime-local" aria-label="结束时间（北京时间）" title="北京时间"><button class="primary-button" @click="addMaintenanceWindow">创建窗口</button></div>
      <div class="backup-list"><div v-for="item in maintenancePagination.pagedItems.value" :key="item.id" class="backup-row"><div><b>{{ item.name }}</b><p>{{ dateTime(item.startsAt) }} — {{ dateTime(item.endsAt) }}</p></div><div><StatusPill :status="item.enabled ? 'available' : 'unknown'" /><button class="tiny danger-button" @click="deleteMaintenanceWindow(item.id)">删除</button></div></div><div v-if="!data.windows.length" class="inline-empty">尚无维护窗口。</div></div>
      <AppPagination v-model:page="maintenancePagination.page.value" v-model:page-size="maintenancePagination.pageSize.value" :total="maintenancePagination.total.value" :page-count="maintenancePagination.pageCount.value" :range-start="maintenancePagination.rangeStart.value" :range-end="maintenancePagination.rangeEnd.value" label="维护窗口分页" />
    </section>

    <template v-else-if="data && tab === 'retention'">
      <div class="section-title"><div><h2>数据保留策略</h2></div><button class="primary-button" @click="saveOperationalSettings">保存保留策略</button></div>
      <div class="settings-grid retention-summary">
        <div><span>基础采集</span><b>{{ data.settings.collectIntervalSeconds }} 秒</b></div><div><span>高级采集</span><b>{{ data.settings.advancedIntervalSeconds }} 秒</b></div><label><span>原始数据（天）</span><input v-model.number="data.settings.rawRetentionDays" type="number" min="1" max="365"></label><label><span>降采样数据（天）</span><input v-model.number="data.settings.rollupRetentionDays" type="number" min="1" max="3650"></label><label><span>审计保留（天）</span><input v-model.number="data.settings.auditRetentionDays" type="number" min="1" max="3650"></label><label><span>巡检保留（天）</span><input v-model.number="data.settings.inspectionRetentionDays" type="number" min="1" max="3650"></label><label><span>数据库备份（份）</span><input v-model.number="data.settings.backupRetentionCount" type="number" min="1" max="100"></label>
      </div>
      <div class="operations-layout">
        <section class="card">
          <div class="section-title"><div><h2>数据库保护</h2></div><button class="primary-button" :disabled="backupLoading" @click="createBackup">{{ backupLoading ? '备份中…' : '立即备份' }}</button></div>
          <div class="database-status"><StatusPill :status="data.database.integrityOk ? 'healthy' : 'critical'" /><b>{{ data.database.integrityOk ? 'SQLite 完整性检查通过' : data.database.integrityError }}</b><span>{{ bytes(data.database.databaseSize) }}</span></div>
          <p v-if="backupEvidence" class="operation-evidence" :class="backupEvidence.status" role="status">{{ backupEvidence.message }}</p>
          <p v-if="restoreEvidence" class="operation-evidence" :class="restoreEvidence.status" role="status">{{ restoreEvidence.message }}</p>
          <div class="backup-list"><div v-for="backup in backupPagination.pagedItems.value" :key="backup.name" class="backup-row"><div><b>{{ backupType(backup.type) }} · v{{ backup.appVersion }}</b><p>{{ dateTime(backup.createdAt) }} · {{ bytes(backup.size) }}</p><code>SHA-256 {{ backup.sha256.slice(0, 16) }}…</code></div><div><StatusPill :status="backup.verified ? 'healthy' : 'critical'" /><button class="tiny secondary-button" :disabled="!backup.verified" @click="restoreBackup(backup.name)">恢复</button><button class="tiny danger-button" @click="deleteBackup(backup.name)">删除</button></div></div><div v-if="!data.backups.length" class="inline-empty">尚无备份。版本升级时会自动创建升级前备份。</div></div>
          <AppPagination v-model:page="backupPagination.page.value" v-model:page-size="backupPagination.pageSize.value" :total="backupPagination.total.value" :page-count="backupPagination.pageCount.value" :range-start="backupPagination.rangeStart.value" :range-end="backupPagination.rangeEnd.value" label="数据库备份分页" />
        </section>
        <aside class="card">
          <div class="section-title compact"><div><h2>7 天稳定性观测</h2></div></div>
          <dl class="definition-list"><div><dt>开始时间</dt><dd>{{ dateTime(data.stability.startedAt) }}</dd></div><div><dt>目标完成</dt><dd>{{ dateTime(data.stability.targetEndAt) }}</dd></div><div><dt>采样 / 失败</dt><dd>{{ data.stability.sampleCount }} / {{ data.stability.failureCount }}</dd></div><div><dt>数据库延迟</dt><dd>{{ data.stability.databaseLatencyMs }} ms</dd></div><div><dt>指标新鲜度</dt><dd>{{ data.stability.metricFreshnessSeconds == null ? '暂无数据' : `${data.stability.metricFreshnessSeconds} 秒` }}</dd></div></dl>
          <p :class="data.stability.qualified ? 'green' : 'amber'">{{ data.stability.qualified ? '已满足连续 7 天无失败资格' : `观测进行中，剩余约 ${duration(data.stability.remainingSeconds)}` }}</p><p v-if="stabilityEvidence" class="operation-evidence" :class="stabilityEvidence.status" role="status">{{ stabilityEvidence.message }}</p><button class="secondary-button" :disabled="stabilityLoading" @click="resetStability">{{ stabilityLoading ? '重置中…' : '重新开始 7 天观测' }}</button>
        </aside>
      </div>
      <section class="card image-cleanup-card">
        <div class="section-title">
          <div>
            <h2>未引用镜像管理</h2>
            <span class="muted">悬空旧镜像可批量清理；带标签的缓存镜像可能供未启动 LPK 使用，只允许逐个确认删除。</span>
          </div>
          <button
            class="danger-button"
            :disabled="imageCleanupLoading || !data.unusedImages.available || data.unusedImages.danglingCount === 0"
            @click="pruneUnusedImages"
          >{{ imageCleanupLoading ? '清理中…' : data.unusedImages.danglingCount ? `清理 ${data.unusedImages.danglingCount} 个悬空镜像` : '没有悬空镜像' }}</button>
        </div>
        <p v-if="imageCleanupEvidence" class="operation-evidence" :class="imageCleanupEvidence.status" role="status">{{ imageCleanupEvidence.message }}</p>
        <p v-if="!data.unusedImages.available" class="operation-evidence warning" role="status">镜像维护不可用：{{ data.unusedImages.error || 'LazyCat Docker 接口未授权或不可用' }}</p>
        <div v-else class="settings-grid image-cleanup-summary">
          <div><span>悬空旧镜像</span><b>{{ data.unusedImages.danglingCount }} 个 · {{ bytes(data.unusedImages.danglingSize) }}</b></div>
          <div><span>未运行缓存镜像</span><b>{{ data.unusedImages.cachedCount }} 个 · {{ bytes(data.unusedImages.cachedSize) }}</b></div>
          <div><span>容器引用保护</span><b>运行和停止容器均保护</b><StatusPill status="available" /></div>
        </div>
        <div class="image-category">
          <div class="section-title compact"><div><h3>悬空旧镜像</h3><span class="muted">没有标签、没有容器引用，可批量或逐个删除。</span></div></div>
          <div v-if="danglingPagination.pagedItems.value.length" class="backup-list">
            <div v-for="image in danglingPagination.pagedItems.value" :key="image.id" class="backup-row">
              <div><b>{{ image.tags.join(', ') }}</b><p>{{ image.id.slice(0, 19) }} · {{ bytes(image.size) }}<template v-if="image.createdAt"> · {{ dateTime(image.createdAt) }}</template></p></div>
              <button class="tiny danger-button" :disabled="deletingImageId === image.id" @click="deleteUnusedImage(image)">{{ deletingImageId === image.id ? '删除中…' : '删除' }}</button>
            </div>
          </div>
          <div v-else class="inline-empty">没有悬空旧镜像。</div>
          <AppPagination v-model:page="danglingPagination.page.value" v-model:page-size="danglingPagination.pageSize.value" :total="danglingPagination.total.value" :page-count="danglingPagination.pageCount.value" :range-start="danglingPagination.rangeStart.value" :range-end="danglingPagination.rangeEnd.value" label="悬空镜像分页" />
        </div>
        <div class="image-category">
          <div class="section-title compact"><div><h3>未运行缓存镜像</h3><span class="muted">当前无容器引用，但可能属于暂停或尚未启动的 LPK。删除后未来启动会重新拉取。</span></div></div>
          <div v-if="cachedPagination.pagedItems.value.length" class="backup-list">
            <div v-for="image in cachedPagination.pagedItems.value" :key="image.id" class="backup-row">
              <div><b>{{ image.tags.join(', ') }}</b><p>{{ image.id.slice(0, 19) }} · {{ bytes(image.size) }}<template v-if="image.createdAt"> · {{ dateTime(image.createdAt) }}</template></p></div>
              <button class="tiny danger-button" :disabled="deletingImageId === image.id" @click="deleteUnusedImage(image)">{{ deletingImageId === image.id ? '删除中…' : '删除并允许重拉' }}</button>
            </div>
          </div>
          <div v-else class="inline-empty">没有未运行缓存镜像。</div>
          <AppPagination v-model:page="cachedPagination.page.value" v-model:page-size="cachedPagination.pageSize.value" :total="cachedPagination.total.value" :page-count="cachedPagination.pageCount.value" :range-start="cachedPagination.rangeStart.value" :range-end="cachedPagination.rangeEnd.value" label="缓存镜像分页" />
        </div>
      </section>
    </template>

    <section v-else-if="data && tab === 'audit'" class="card">
      <div class="section-title"><div><h2>用户与审计</h2></div></div>
      <div class="settings-grid"><div><span>用户模式</span><b>单用户</b><StatusPill status="available" /></div><div><span>审计保留</span><b>{{ data.settings.auditRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>巡检保留</span><b>{{ data.settings.inspectionRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>审计记录</span><b>{{ data.audit.length }} 条</b><StatusPill status="available" /></div></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>时间</th><th>操作</th><th>对象</th><th>详情</th></tr></thead><tbody><tr v-for="item in auditPagination.pagedItems.value" :key="item.id"><td>{{ dateTime(item.createdAt) }}</td><td><code>{{ item.action }}</code></td><td>{{ item.subjectType }} · {{ item.subjectId }}</td><td><code>{{ JSON.stringify(item.metadata) }}</code></td></tr></tbody></table></div>
      <AppPagination v-model:page="auditPagination.page.value" v-model:page-size="auditPagination.pageSize.value" :total="auditPagination.total.value" :page-count="auditPagination.pageCount.value" :range-start="auditPagination.rangeStart.value" :range-end="auditPagination.rangeEnd.value" label="审计记录分页" />
    </section>
    </div>
  </PageState>
</template>
