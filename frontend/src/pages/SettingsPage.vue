<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling, useRovingTabs } from '@/composables'
import type { Backup, Capability, Stability } from '@/types'
import { ago, backupType, bytes, dateTime, duration } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Settings {
  appVersion: string; deploymentMode: string; embeddedCollector: boolean; singleUser: boolean; maxDevices: number
  collectIntervalSeconds: number; advancedIntervalSeconds: number; rawRetentionDays: number; rollupRetentionDays: number
  auditRetentionDays: number; inspectionRetentionDays: number; notificationChannel: string; notificationDelivery: string
  storageStats: { rawMetricRows: number; rollupRows: number }
}
interface Operations { capabilities: Capability[]; schedule: { daily: { hour: number }; weekly: { hour: number }; timezone: string } }
interface DatabaseStatus { databaseSize: number; integrityOk: boolean; integrityError?: string; backupCount: number; latestBackup?: Backup }
interface Payload { settings: Settings; operations: Operations; database: DatabaseStatus; backups: Backup[]; stability: Stability }
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
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [settings, operations, database, backups, stability] = await Promise.all([
    api<Settings>('/api/v1/settings'), api<Operations>('/api/v1/operations'), api<DatabaseStatus>('/api/v1/database/status'),
    api<{ items: Backup[] }>('/api/v1/backups'), api<Stability>('/api/v1/stability'),
  ])
  return {
    settings,
    operations: { ...operations, capabilities: operations.capabilities || [] },
    database,
    backups: backups.items || [],
    stability,
  }
})
const localCapability = computed(() => data.value?.operations.capabilities.filter((item) => !item.capability.startsWith('remote.')) || [])

async function createPairingCode() {
  pairingLoading.value = true
  try {
    pairing.value = await api<PairingCode>('/api/v1/pairing-codes', { method: 'POST' })
    emit('toast', '一次性配对码已生成')
  } catch (reason) { emit('toast', reason instanceof Error ? reason.message : String(reason)) }
  finally { pairingLoading.value = false }
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

    <section v-else-if="data && tab === 'groups'" class="card"><div class="section-title"><div><h2>设备组与标签</h2><span class="muted">Fleet inventory organization</span></div></div><div class="contract-empty"><b>Contract gap</b><p>当前后端尚未提供设备组、位置和标签的读写契约，因此此处不伪造原型示例。</p></div></section>

    <section v-else-if="data && tab === 'capabilities'" class="card">
      <div class="section-title"><div><h2>Collector 能力</h2><span class="muted">每项状态来自最近一次真实能力检查</span></div></div>
      <div class="capability-list"><div v-for="item in localCapability" :key="`${item.capability}-${item.checkedAt}`"><div><b>{{ item.capability }}</b><p>{{ item.detail }}</p><small>检查于 {{ ago(item.checkedAt) }}</small></div><StatusPill :status="item.status || 'unknown'" /></div></div>
    </section>

    <section v-else-if="data && tab === 'thresholds'" class="card"><div class="section-title"><div><h2>告警阈值</h2><span class="muted">Rule configuration</span></div></div><div class="contract-empty"><b>只读 · Contract gap</b><p>当前告警规则由服务端生产配置执行，但尚无阈值查询/修改 API。页面不会硬编码代码中的阈值冒充可配置项。</p></div></section>

    <section v-else-if="data && tab === 'notifications'" class="card">
      <div class="section-title"><div><h2>通知渠道</h2><span class="muted">告警新发、升级、恢复与巡检结果</span></div></div>
      <div class="settings-grid"><div><span>当前渠道</span><b>{{ data.settings.notificationChannel === 'lazycat' ? 'LazyCat 系统通知' : data.settings.notificationChannel }}</b><StatusPill status="available" /></div><div><span>投递策略</span><b>{{ data.settings.notificationDelivery === 'outbox-retry' ? '持久队列重试' : data.settings.notificationDelivery }}</b><StatusPill status="available" /></div><div><span>待发送</span><b>{{ data.stability.pendingNotifications }}</b><StatusPill :status="data.stability.pendingNotifications ? 'warning' : 'healthy'" /></div></div>
    </section>

    <section v-else-if="data && tab === 'maintenance'" class="card">
      <div class="section-title"><div><h2>维护窗口与巡检计划</h2><span class="muted">Automatic inspection schedule</span></div></div>
      <div class="settings-grid"><div><span>每日巡检</span><b>{{ data.operations.schedule.daily.hour }}:00</b><StatusPill status="available" /></div><div><span>每周巡检</span><b>周日 {{ data.operations.schedule.weekly.hour }}:00</b><StatusPill status="available" /></div><div><span>时区</span><b>{{ data.operations.schedule.timezone }}</b><StatusPill status="available" /></div></div>
      <div class="contract-empty small"><b>维护窗口 Contract gap</b><p>当前 API 未提供告警抑制维护窗口配置。</p></div>
    </section>

    <template v-else-if="data && tab === 'retention'">
      <div class="settings-grid retention-summary">
        <div><span>基础采集</span><b>{{ data.settings.collectIntervalSeconds }} 秒</b></div><div><span>高级采集</span><b>{{ data.settings.advancedIntervalSeconds }} 秒</b></div><div><span>原始数据</span><b>{{ data.settings.rawRetentionDays }} 天</b></div><div><span>降采样数据</span><b>{{ data.settings.rollupRetentionDays }} 天</b></div>
      </div>
      <div class="operations-layout">
        <section class="card">
          <div class="section-title"><div><h2>数据库保护</h2><span class="muted">在线备份、完整性检查和恢复</span></div><button class="primary-button" :disabled="backupLoading" @click="createBackup">{{ backupLoading ? '备份中…' : '立即备份' }}</button></div>
          <div class="database-status"><StatusPill :status="data.database.integrityOk ? 'healthy' : 'critical'" /><b>{{ data.database.integrityOk ? 'SQLite 完整性检查通过' : data.database.integrityError }}</b><span>{{ bytes(data.database.databaseSize) }}</span></div>
          <p v-if="backupEvidence" class="operation-evidence" :class="backupEvidence.status" role="status">{{ backupEvidence.message }}</p>
          <p v-if="restoreEvidence" class="operation-evidence" :class="restoreEvidence.status" role="status">{{ restoreEvidence.message }}</p>
          <div class="backup-list"><div v-for="backup in data.backups.slice(0, 8)" :key="backup.name" class="backup-row"><div><b>{{ backupType(backup.type) }} · v{{ backup.appVersion }}</b><p>{{ dateTime(backup.createdAt) }} · {{ bytes(backup.size) }}</p><code>SHA-256 {{ backup.sha256.slice(0, 16) }}…</code></div><div><StatusPill :status="backup.verified ? 'healthy' : 'critical'" /><button class="tiny danger-button" :disabled="!backup.verified" @click="restoreBackup(backup.name)">恢复</button></div></div><div v-if="!data.backups.length" class="inline-empty">尚无备份。版本升级时会自动创建升级前备份。</div></div>
        </section>
        <aside class="card">
          <div class="section-title compact"><div><h2>7 天稳定性观测</h2><span class="muted">长期生产验证</span></div></div>
          <dl class="definition-list"><div><dt>开始时间</dt><dd>{{ dateTime(data.stability.startedAt) }}</dd></div><div><dt>目标完成</dt><dd>{{ dateTime(data.stability.targetEndAt) }}</dd></div><div><dt>采样 / 失败</dt><dd>{{ data.stability.sampleCount }} / {{ data.stability.failureCount }}</dd></div><div><dt>数据库延迟</dt><dd>{{ data.stability.databaseLatencyMs }} ms</dd></div><div><dt>指标新鲜度</dt><dd>{{ data.stability.metricFreshnessSeconds == null ? 'Unknown' : `${data.stability.metricFreshnessSeconds} 秒` }}</dd></div></dl>
          <p :class="data.stability.qualified ? 'green' : 'amber'">{{ data.stability.qualified ? '已满足连续 7 天无失败资格' : `观测进行中，剩余约 ${duration(data.stability.remainingSeconds)}` }}</p><p v-if="stabilityEvidence" class="operation-evidence" :class="stabilityEvidence.status" role="status">{{ stabilityEvidence.message }}</p><button class="secondary-button" :disabled="stabilityLoading" @click="resetStability">{{ stabilityLoading ? '重置中…' : '重新开始 7 天观测' }}</button>
        </aside>
      </div>
    </template>

    <section v-else-if="data && tab === 'audit'" class="card">
      <div class="section-title"><div><h2>用户与审计</h2><span class="muted">Single-user production boundary</span></div></div>
      <div class="settings-grid"><div><span>用户模式</span><b>单用户</b><StatusPill status="available" /></div><div><span>审计保留</span><b>{{ data.settings.auditRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>巡检保留</span><b>{{ data.settings.inspectionRetentionDays }} 天</b><StatusPill status="available" /></div><div><span>审计浏览器</span><b>Contract gap</b><StatusPill status="unknown" /></div></div>
    </section>
    </div>
  </PageState>
</template>
