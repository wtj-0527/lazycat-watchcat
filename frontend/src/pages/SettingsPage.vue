<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling, useRovingTabs } from '@/composables'
import type { Backup, Capability, Device, Stability } from '@/types'
import { ago, backupType, bytes, dateTime, duration } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

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
  devices: Device[]; rules: AlertRule[]; windows: MaintenanceWindow[]; audit: AuditEntry[]; unusedImages: UnusedImages
}
interface PairingCode { code: string; expiresAt: string }
interface RestoreResult { status: string; backup: string; message: string }
interface OperationEvidence { status: 'success' | 'warning' | 'error'; message: string }
type Tab = 'onboarding' | 'groups' | 'capabilities' | 'thresholds' | 'notifications' | 'maintenance' | 'retention' | 'audit'
const tabs: Array<[Tab, string]> = [
  ['onboarding', '设备接入'], ['groups', '设备组与标签'], ['capabilities', 'Collector 能力'], ['thresholds', '告警阈值'],
  ['notifications', '通知渠道'], ['maintenance', '维护窗口'], ['retention', '数据保留'], ['audit', '用户与审计'],
]
const props = defineProps<{ initialTab?: Tab }>()
const isOnboardingRoute = computed(() => props.initialTab === 'onboarding')
const emit = defineEmits<{ toast: [message: string] }>()
const { selected: tab, select: selectTab, move: moveTab } = useRovingTabs(tabs, props.initialTab || 'onboarding', 'settings-tab-')
const pairing = ref<PairingCode>()
const pairingLoading = ref(false)
const backupLoading = ref(false)
const backupEvidence = ref<OperationEvidence>()
const restoreEvidence = ref<OperationEvidence>()
const stabilityLoading = ref(false)
const stabilityEvidence = ref<OperationEvidence>()
const imageCleanupLoading = ref(false)
const deletingImageId = ref('')
const imageCleanupEvidence = ref<OperationEvidence>()
const danglingPage = ref(1)
const cachedPage = ref(1)
const settingsEvidence = ref<OperationEvidence>()
const maintenanceName = ref('')
const maintenanceStart = ref('')
const maintenanceEnd = ref('')
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [settings, operations, database, backups, stability, devices, rules, windows, audit, unusedImages] = await Promise.all([
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
  }
})
const localCapability = computed(() => data.value?.operations.capabilities.filter((item) => !item.capability.startsWith('remote.')) || [])
const imagePageSize = 8
const danglingImages = computed(() => data.value?.unusedImages.items.filter((item) => item.category === 'dangling') || [])
const cachedImages = computed(() => data.value?.unusedImages.items.filter((item) => item.category === 'cached') || [])
const danglingPages = computed(() => Math.max(1, Math.ceil(danglingImages.value.length / imagePageSize)))
const cachedPages = computed(() => Math.max(1, Math.ceil(cachedImages.value.length / imagePageSize)))
const visibleDanglingImages = computed(() => {
  const page = Math.min(danglingPage.value, danglingPages.value)
  return danglingImages.value.slice((page - 1) * imagePageSize, page * imagePageSize)
})
const visibleCachedImages = computed(() => {
  const page = Math.min(cachedPage.value, cachedPages.value)
  return cachedImages.value.slice((page - 1) * imagePageSize, page * imagePageSize)
})

async function createPairingCode() {
  pairingLoading.value = true
  try {
    pairing.value = await api<PairingCode>('/api/v1/pairing-codes', { method: 'POST' })
    emit('toast', '一次性配对码已生成')
  } catch (reason) { emit('toast', reason instanceof Error ? reason.message : String(reason)) }
  finally { pairingLoading.value = false }
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
  await api('/api/v1/maintenance-windows', {
    method: 'POST',
    body: JSON.stringify({ name: maintenanceName.value, startsAt: new Date(maintenanceStart.value).toISOString(), endsAt: new Date(maintenanceEnd.value).toISOString(), enabled: true }),
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
  if (!window.confirm(`确定删除备份 ${name}？删除后无法恢复。`)) return
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
  if (!window.confirm('恢复将重启猫眼并造成短暂断连；替换前会再创建安全备份。确定继续？')) return
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
  if (!window.confirm('确定清零当前稳定性观测并重新计算 7 天周期？')) return
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
  if (!window.confirm(
    `确定清理 ${preview.danglingCount} 个悬空旧镜像？\n\n镜像逻辑大小约 ${bytes(preview.danglingSize)}。带标签、可能供暂停应用未来启动使用的缓存镜像不会批量删除。`,
  )) return
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
  if (!window.confirm(`确定删除镜像 ${label}？\n\n${warning}`)) return
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
    danglingPage.value = Math.min(danglingPage.value, danglingPages.value)
    cachedPage.value = Math.min(cachedPage.value, cachedPages.value)
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
      <div>
        <h2>{{ isOnboardingRoute ? '接入新设备' : '设置与治理' }}</h2>
        <p>{{ isOnboardingRoute ? '一次完成配对、连通性验证、能力探测和权限修复。' : '所有变更均记录操作、时间与服务端回读结果。' }}</p>
      </div>
    </div>
    <div v-if="isOnboardingRoute" class="onboarding-progress" aria-label="接入步骤">
      <div class="done"><b>1</b><span>选择方式</span></div>
      <div class="done"><b>2</b><span>生成配对码</span></div>
      <div class="active"><b>3</b><span>验证连接</span></div>
      <div><b>4</b><span>能力探测</span></div>
      <div><b>5</b><span>完成</span></div>
    </div>
    <div v-else class="settings-tabs" role="tablist" aria-label="设置分类">
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

    <div v-if="data" id="settings-panel" role="tabpanel" :aria-labelledby="`settings-tab-${tab}`">
    <p v-if="settingsEvidence" class="operation-evidence" :class="settingsEvidence.status" role="status">{{ settingsEvidence.message }}</p>

    <template v-if="data && tab === 'onboarding'">
      <div class="onboarding-layout">
        <section class="card">
          <div class="section-title"><div><h2>验证设备连接</h2><span class="muted">在目标 LazyCat 设备安装猫眼后输入配对码；连接成功后自动进行安全握手。</span></div></div>
          <ol class="onboarding-steps">
            <li><span>1</span><div><b>配对请求</b><p>远端 Collector 使用 mTLS 上报；当前猫眼 LPK 已内置本机 Collector。</p></div><StatusPill :status="data.settings.embeddedCollector ? 'available' : 'unknown'" /></li>
            <li><span>2</span><div><b>一次性配对码</b><p>配对码由真实 API 生成，只能使用一次并具有到期时间。</p></div><button class="primary-button" :disabled="pairingLoading" @click="createPairingCode">{{ pairingLoading ? '生成中…' : '生成配对码' }}</button></li>
            <li><span>3</span><div><b>首次数据上报</b><p>完成身份验证后等待第一批真实系统指标。</p></div><StatusPill status="unknown" /></li>
          </ol>
          <div v-if="pairing" class="pairing-code-box"><span>一次性配对码</span><strong>{{ pairing.code }}</strong><small>有效期至 {{ dateTime(pairing.expiresAt) }}</small></div>
        </section>
        <aside class="card deployment-card">
          <div class="section-title compact"><div><h2>当前部署</h2><span class="muted">Production profile</span></div></div>
          <dl class="definition-list">
            <div><dt>应用版本</dt><dd>v{{ data.settings.appVersion }}</dd></div>
            <div><dt>部署模式</dt><dd>单一 LPK</dd></div>
            <div><dt>本机 Collector</dt><dd>{{ data.settings.embeddedCollector ? '已内置' : '未启用' }}</dd></div>
            <div><dt>用户模式</dt><dd>{{ data.settings.singleUser ? '单用户' : '多用户' }}</dd></div>
            <div><dt>设备上限</dt><dd>{{ data.settings.maxDevices }}</dd></div>
          </dl>
          <p class="production-note">仅安装一个猫眼 LPK；Hub、Web UI、SQLite、告警、巡检、通知和本机 Collector 同包运行。</p>
        </aside>
      </div>
    </template>

    <section v-else-if="data && tab === 'groups'" class="card">
      <div class="section-title"><div><h2>设备组与标签</h2></div></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>设备</th><th>设备组</th><th>位置</th><th>标签</th><th /></tr></thead><tbody>
        <tr v-for="device in data.devices" :key="device.id">
          <td><b>{{ device.name }}</b><small>{{ device.hostname }}</small></td>
          <td><input v-model="device.group" aria-label="设备组"></td>
          <td><input v-model="device.location" aria-label="位置"></td>
          <td><input :value="Object.entries(device.labels || {}).map(([key, value]) => `${key}=${value}`).join(', ')" aria-label="标签" @change="updateLabels(device, $event)"></td>
          <td><button class="secondary-button tiny" @click="saveDeviceMetadata(device)">保存</button></td>
        </tr>
      </tbody></table></div>
    </section>

    <section v-else-if="data && tab === 'capabilities'" class="card">
      <div class="section-title"><div><h2>Collector 能力</h2><span class="muted">每项状态来自最近一次真实能力检查</span></div></div>
      <div class="capability-list"><div v-for="item in localCapability" :key="`${item.capability}-${item.checkedAt}`"><div><b>{{ item.capability }}</b><p>{{ item.detail }}</p><small>检查于 {{ ago(item.checkedAt) }}</small></div><StatusPill :status="item.status || 'unknown'" /></div></div>
    </section>

    <section v-else-if="data && tab === 'thresholds'" class="card">
      <div class="section-title"><div><h2>告警阈值</h2></div><button class="primary-button" @click="saveRules">保存并重新评估</button></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>规则</th><th>指标</th><th>Warning</th><th>Critical</th><th>启用</th></tr></thead><tbody>
        <tr v-for="rule in data.rules" :key="rule.metric"><td><b>{{ rule.label }}</b></td><td><code>{{ rule.metric }}</code></td><td><input v-model.number="rule.warning" type="number" min="0"></td><td><input v-model.number="rule.critical" type="number" min="0"></td><td><input v-model="rule.enabled" type="checkbox"></td></tr>
      </tbody></table></div>
    </section>

    <section v-else-if="data && tab === 'notifications'" class="card">
      <div class="section-title"><div><h2>通知渠道</h2><span class="muted">告警新发、升级、恢复与巡检结果</span></div><button class="secondary-button" @click="sendTestNotification">发送测试通知</button></div>
      <div class="settings-grid"><div><span>当前渠道</span><b>{{ data.settings.notificationChannel === 'lazycat' ? 'LazyCat 系统通知' : data.settings.notificationChannel }}</b><StatusPill status="available" /></div><div><span>投递策略</span><b>{{ data.settings.notificationDelivery === 'outbox-retry' ? '持久队列重试' : data.settings.notificationDelivery }}</b><StatusPill status="available" /></div><div><span>待发送</span><b>{{ data.stability.pendingNotifications }}</b><StatusPill :status="data.stability.pendingNotifications ? 'warning' : 'healthy'" /></div></div>
    </section>

    <section v-else-if="data && tab === 'maintenance'" class="card">
      <div class="section-title"><div><h2>维护窗口与巡检计划</h2></div><button class="primary-button" @click="saveOperationalSettings">保存计划</button></div>
      <div class="settings-grid"><label><span>每日巡检小时</span><input v-model.number="data.settings.dailyInspectionHour" type="number" min="0" max="23"></label><label><span>每周日巡检小时</span><input v-model.number="data.settings.weeklyInspectionHour" type="number" min="0" max="23"></label><div><span>时区</span><b>{{ data.operations.schedule.timezone }}</b><StatusPill status="available" /></div></div>
      <div class="maintenance-form"><input v-model="maintenanceName" placeholder="窗口名称"><input v-model="maintenanceStart" type="datetime-local" aria-label="开始时间"><input v-model="maintenanceEnd" type="datetime-local" aria-label="结束时间"><button class="primary-button" @click="addMaintenanceWindow">创建窗口</button></div>
      <div class="backup-list"><div v-for="item in data.windows" :key="item.id" class="backup-row"><div><b>{{ item.name }}</b><p>{{ dateTime(item.startsAt) }} — {{ dateTime(item.endsAt) }}</p></div><div><StatusPill :status="item.enabled ? 'available' : 'unknown'" /><button class="tiny danger-button" @click="deleteMaintenanceWindow(item.id)">删除</button></div></div><div v-if="!data.windows.length" class="inline-empty">尚无维护窗口。</div></div>
    </section>

    <template v-else-if="data && tab === 'retention'">
      <div class="section-title"><div><h2>数据保留策略</h2></div><button class="primary-button" @click="saveOperationalSettings">保存保留策略</button></div>
      <div class="settings-grid retention-summary">
        <div><span>基础采集</span><b>{{ data.settings.collectIntervalSeconds }} 秒</b></div><div><span>高级采集</span><b>{{ data.settings.advancedIntervalSeconds }} 秒</b></div><label><span>原始数据（天）</span><input v-model.number="data.settings.rawRetentionDays" type="number" min="1" max="365"></label><label><span>降采样数据（天）</span><input v-model.number="data.settings.rollupRetentionDays" type="number" min="1" max="3650"></label><label><span>审计保留（天）</span><input v-model.number="data.settings.auditRetentionDays" type="number" min="1" max="3650"></label><label><span>巡检保留（天）</span><input v-model.number="data.settings.inspectionRetentionDays" type="number" min="1" max="3650"></label><label><span>数据库备份（份）</span><input v-model.number="data.settings.backupRetentionCount" type="number" min="1" max="100"></label>
      </div>
      <div class="operations-layout">
        <section class="card">
          <div class="section-title"><div><h2>数据库保护</h2><span class="muted">在线备份、完整性检查和恢复</span></div><button class="primary-button" :disabled="backupLoading" @click="createBackup">{{ backupLoading ? '备份中…' : '立即备份' }}</button></div>
          <div class="database-status"><StatusPill :status="data.database.integrityOk ? 'healthy' : 'critical'" /><b>{{ data.database.integrityOk ? 'SQLite 完整性检查通过' : data.database.integrityError }}</b><span>{{ bytes(data.database.databaseSize) }}</span></div>
          <p v-if="backupEvidence" class="operation-evidence" :class="backupEvidence.status" role="status">{{ backupEvidence.message }}</p>
          <p v-if="restoreEvidence" class="operation-evidence" :class="restoreEvidence.status" role="status">{{ restoreEvidence.message }}</p>
          <div class="backup-list"><div v-for="backup in data.backups" :key="backup.name" class="backup-row"><div><b>{{ backupType(backup.type) }} · v{{ backup.appVersion }}</b><p>{{ dateTime(backup.createdAt) }} · {{ bytes(backup.size) }}</p><code>SHA-256 {{ backup.sha256.slice(0, 16) }}…</code></div><div><StatusPill :status="backup.verified ? 'healthy' : 'critical'" /><button class="tiny secondary-button" :disabled="!backup.verified" @click="restoreBackup(backup.name)">恢复</button><button class="tiny danger-button" @click="deleteBackup(backup.name)">删除</button></div></div><div v-if="!data.backups.length" class="inline-empty">尚无备份。版本升级时会自动创建升级前备份。</div></div>
        </section>
        <aside class="card">
          <div class="section-title compact"><div><h2>7 天稳定性观测</h2><span class="muted">长期生产验证</span></div></div>
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
          <div v-if="visibleDanglingImages.length" class="backup-list">
            <div v-for="image in visibleDanglingImages" :key="image.id" class="backup-row">
              <div><b>{{ image.tags.join(', ') }}</b><p>{{ image.id.slice(0, 19) }} · {{ bytes(image.size) }}<template v-if="image.createdAt"> · {{ dateTime(image.createdAt) }}</template></p></div>
              <button class="tiny danger-button" :disabled="deletingImageId === image.id" @click="deleteUnusedImage(image)">{{ deletingImageId === image.id ? '删除中…' : '删除' }}</button>
            </div>
          </div>
          <div v-else class="inline-empty">没有悬空旧镜像。</div>
          <div v-if="danglingPages > 1" class="pagination"><button :disabled="danglingPage <= 1" @click="danglingPage--">上一页</button><span>{{ danglingPage }} / {{ danglingPages }}</span><button :disabled="danglingPage >= danglingPages" @click="danglingPage++">下一页</button></div>
        </div>
        <div class="image-category">
          <div class="section-title compact"><div><h3>未运行缓存镜像</h3><span class="muted">当前无容器引用，但可能属于暂停或尚未启动的 LPK。删除后未来启动会重新拉取。</span></div></div>
          <div v-if="visibleCachedImages.length" class="backup-list">
            <div v-for="image in visibleCachedImages" :key="image.id" class="backup-row">
              <div><b>{{ image.tags.join(', ') }}</b><p>{{ image.id.slice(0, 19) }} · {{ bytes(image.size) }}<template v-if="image.createdAt"> · {{ dateTime(image.createdAt) }}</template></p></div>
              <button class="tiny danger-button" :disabled="deletingImageId === image.id" @click="deleteUnusedImage(image)">{{ deletingImageId === image.id ? '删除中…' : '删除并允许重拉' }}</button>
            </div>
          </div>
          <div v-else class="inline-empty">没有未运行缓存镜像。</div>
          <div v-if="cachedPages > 1" class="pagination"><button :disabled="cachedPage <= 1" @click="cachedPage--">上一页</button><span>{{ cachedPage }} / {{ cachedPages }}</span><button :disabled="cachedPage >= cachedPages" @click="cachedPage++">下一页</button></div>
        </div>
      </section>
    </template>

    <section v-else-if="data && tab === 'audit'" class="card">
      <div class="section-title"><div><h2>用户与审计</h2><span class="muted">Single-user production boundary</span></div></div>
      <div class="settings-grid"><div><span>用户模式</span><b>单用户</b><StatusPill status="available" /></div><div><span>审计保留</span><b>{{ data.settings.auditRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>巡检保留</span><b>{{ data.settings.inspectionRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>审计记录</span><b>{{ data.audit.length }} 条</b><StatusPill status="available" /></div></div>
      <div class="table-scroll"><table class="fleet-table"><thead><tr><th>时间</th><th>操作</th><th>对象</th><th>详情</th></tr></thead><tbody><tr v-for="item in data.audit" :key="item.id"><td>{{ dateTime(item.createdAt) }}</td><td><code>{{ item.action }}</code></td><td>{{ item.subjectType }} · {{ item.subjectId }}</td><td><code>{{ JSON.stringify(item.metadata) }}</code></td></tr></tbody></table></div>
    </section>
    </div>
  </PageState>
</template>
