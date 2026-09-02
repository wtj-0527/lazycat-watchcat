<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Alert } from '@/types'
import { ago, dateTime } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatusPill from '@/components/StatusPill.vue'

interface NotificationSettings {
  enabled: boolean
  criticalAlerts: boolean
  warningAlerts: boolean
  recoveryNotifications: boolean
  inspectionResults: boolean
  cooldownMinutes: number
  quietHoursEnabled: boolean
  quietStart: string
  quietEnd: string
  recipientMode: 'admins' | 'selected'
  recipientKeys: string[]
}
interface NotificationRecipient {
  key: string
  deviceId: string
  deviceName: string
  userId: string
  nickname: string
  role: string
  online: boolean
}
interface Payload {
  settings: NotificationSettings
  summary: { pending: number; sent: number; failed: number; total: number }
  acceptedRisks: Array<Alert & { acceptedAt?: string; acceptedUntil?: string }>
  channel: string
  delivery: string
  recipients: NotificationRecipient[]
}

const emit = defineEmits<{ toast: [message: string] }>()
const saving = ref(false)
const testing = ref(false)
const { data, loading, error, refresh } = usePolling(() => api<Payload>('/api/v1/notification-settings'))
const effectiveRecipients = computed(() => {
  if (!data.value) return []
  return data.value.settings.recipientMode === 'admins'
    ? data.value.recipients.filter((item) => item.role === 'admin')
    : data.value.recipients.filter((item) => data.value?.settings.recipientKeys.includes(item.key))
})
const recipientGroups = computed(() => {
  const groups = new Map<string, { id: string; name: string; users: NotificationRecipient[] }>()
  for (const recipient of data.value?.recipients || []) {
    const current = groups.get(recipient.deviceId) || { id: recipient.deviceId, name: recipient.deviceName || recipient.deviceId, users: [] }
    current.users.push(recipient)
    groups.set(recipient.deviceId, current)
  }
  return [...groups.values()]
})

function toggle(key: keyof NotificationSettings) {
  if (!data.value || typeof data.value.settings[key] !== 'boolean') return
  ;(data.value.settings[key] as boolean) = !(data.value.settings[key] as boolean)
}
function selectRecipientMode(mode: 'admins' | 'selected') {
  if (!data.value) return
  data.value.settings.recipientMode = mode
  if (mode === 'selected' && !data.value.settings.recipientKeys.length) {
    data.value.settings.recipientKeys = data.value.recipients.filter((item) => item.role === 'admin').map((item) => item.key)
  }
}
function toggleRecipient(key: string) {
  if (!data.value) return
  const keys = data.value.settings.recipientKeys
  data.value.settings.recipientKeys = keys.includes(key) ? keys.filter((item) => item !== key) : [...keys, key]
}
async function save() {
  if (!data.value || saving.value) return
  saving.value = true
  try {
    if (data.value.settings.recipientMode === 'selected' && !data.value.settings.recipientKeys.length) {
      emit('toast', '请至少选择一位通知接收人')
      return
    }
    await api('/api/v1/notification-settings', { method: 'PUT', body: JSON.stringify(data.value.settings) })
    await refresh()
    emit('toast', '通知设置已保存')
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    saving.value = false
  }
}
async function testNotification() {
  testing.value = true
  try {
    await api('/api/v1/notifications/test', { method: 'POST' })
    emit('toast', '测试通知已进入发送队列')
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    testing.value = false
  }
}
async function cancelAccepted(alert: Alert) {
  await api(`/api/v1/alerts/${encodeURIComponent(alert.fingerprint)}/unaccept`, { method: 'POST' })
  await refresh()
  emit('toast', '已取消接受风险，告警将重新参与通知')
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <template v-if="data">
      <div class="page-intro notification-page-heading">
        <div><h2>通知设置</h2><p>集中管理发送范围、免打扰时间和已接受风险。</p></div>
        <div class="notification-heading-actions">
          <button class="secondary-button" :disabled="testing || !data.settings.enabled" @click="testNotification">{{ testing ? '发送中…' : '发送测试通知' }}</button>
          <button class="primary-button" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存设置' }}</button>
        </div>
      </div>

      <section class="notification-master" :class="{ disabled: !data.settings.enabled }">
        <div><span class="notification-master-icon">◉</span><div><b>LazyCat 系统通知</b><small>{{ data.settings.enabled ? '通知总开关已开启' : '所有新通知已停止，监控和告警记录不受影响' }}</small></div></div>
        <button class="toggle-control" :class="{ active: data.settings.enabled }" :aria-pressed="data.settings.enabled" @click="toggle('enabled')"><i /><span>{{ data.settings.enabled ? '已开启' : '已关闭' }}</span></button>
      </section>

      <div class="notification-summary-grid">
        <div><span>通知接收人</span><b>{{ effectiveRecipients.length }} 人</b><small>{{ data.settings.recipientMode === 'admins' ? '仅通知各设备管理员' : '仅通知已选择的用户' }}</small></div>
        <div><span>等待发送</span><b>{{ data.summary.pending }}</b><small>关闭总开关后不会继续发送</small></div>
        <div><span>已发送</span><b>{{ data.summary.sent }}</b><small>历史累计成功投递</small></div>
        <div><span>发送失败</span><b>{{ data.summary.failed }}</b><StatusPill :status="data.summary.failed ? 'warning' : 'healthy'" /></div>
      </div>

      <section class="notification-section notification-recipient-section">
        <div class="section-title">
          <div><h2>通知接收人</h2><p>通知只发送给对应用户的在线终端，不再向设备上的所有用户广播。</p></div>
          <b>{{ effectiveRecipients.length }} 人</b>
        </div>
        <div class="recipient-mode-switch" role="group" aria-label="通知接收范围">
          <button :class="{ active: data.settings.recipientMode === 'admins' }" @click="selectRecipientMode('admins')"><b>仅管理员</b><small>自动通知每台设备的管理员</small></button>
          <button :class="{ active: data.settings.recipientMode === 'selected' }" @click="selectRecipientMode('selected')"><b>指定用户</b><small>按设备精确选择通知对象</small></button>
        </div>
        <div v-if="data.settings.recipientMode === 'selected'" class="notification-recipient-groups">
          <article v-for="group in recipientGroups" :key="group.id">
            <header><div><b>{{ group.name }}</b><small>{{ group.users.length }} 位用户</small></div></header>
            <div>
              <button
                v-for="recipient in group.users"
                :key="recipient.key"
                class="notification-recipient"
                :class="{ selected: data.settings.recipientKeys.includes(recipient.key) }"
                :aria-pressed="data.settings.recipientKeys.includes(recipient.key)"
                @click="toggleRecipient(recipient.key)"
              >
                <i>{{ (recipient.nickname || recipient.userId).slice(0, 1).toUpperCase() }}</i>
                <span><b>{{ recipient.nickname || recipient.userId }}</b><small>{{ recipient.userId }} · {{ recipient.role === 'admin' ? '管理员' : '普通用户' }}</small></span>
                <em :class="{ online: recipient.online }">{{ recipient.online ? '在线' : '离线' }}</em>
                <strong><span /></strong>
              </button>
            </div>
          </article>
          <div v-if="!recipientGroups.length" class="inline-empty">尚未采集到用户信息，请先在“用户”页面刷新设备用户。</div>
        </div>
        <div v-else class="recipient-admin-note">
          <span>✓</span><div><b>管理员自动同步</b><small>新增或调整管理员后会自动更新接收范围；普通用户不会收到通知。</small></div>
        </div>
      </section>

      <div class="notification-settings-layout">
        <section class="notification-section">
          <div class="section-title compact"><div><h2>通知内容</h2><p>关闭某一类只影响通知，不会关闭采集、告警和审计。</p></div></div>
          <div class="notification-option-list">
            <button v-for="item in [
              { key: 'criticalAlerts', title: '严重告警', text: '设备、磁盘或服务达到严重阈值时通知' },
              { key: 'warningAlerts', title: '警告告警', text: '达到警告阈值时通知' },
              { key: 'recoveryNotifications', title: '恢复通知', text: '告警证据恢复正常时发送一次通知' },
              { key: 'inspectionResults', title: '巡检结果', text: '正式巡检发现严重问题时通知' },
            ]" :key="item.key" @click="toggle(item.key as keyof NotificationSettings)">
              <span><b>{{ item.title }}</b><small>{{ item.text }}</small></span>
              <i class="mini-toggle" :class="{ active: data.settings[item.key as keyof NotificationSettings] }"><em /></i>
            </button>
          </div>
        </section>

        <section class="notification-section">
          <div class="section-title compact"><div><h2>频率与免打扰</h2><p>时间统一使用北京时间。</p></div></div>
          <div class="notification-form">
            <label><span>同一告警最短间隔</span><select v-model.number="data.settings.cooldownMinutes"><option :value="5">5 分钟</option><option :value="10">10 分钟</option><option :value="30">30 分钟</option><option :value="60">1 小时</option><option :value="360">6 小时</option><option :value="1440">24 小时</option></select></label>
            <button class="quiet-hours-toggle" :class="{ active: data.settings.quietHoursEnabled }" @click="toggle('quietHoursEnabled')"><span><b>免打扰时段</b><small>期间产生的通知将在结束后发送</small></span><i><em /></i></button>
            <div v-if="data.settings.quietHoursEnabled" class="quiet-hours-range"><label><span>开始</span><input v-model="data.settings.quietStart" type="time"></label><i>至</i><label><span>结束</span><input v-model="data.settings.quietEnd" type="time"></label></div>
          </div>
        </section>
      </div>

      <section class="notification-section accepted-risk-section">
        <div class="section-title">
          <div><h2>已接受风险</h2><p>这些风险仍会显示和记录，但不会再发送触发、升级或恢复通知。</p></div>
          <b>{{ data.acceptedRisks.length }} 项</b>
        </div>
        <div v-if="data.acceptedRisks.length" class="accepted-risk-list">
          <article v-for="alert in data.acceptedRisks" :key="alert.fingerprint">
            <StatusPill :status="alert.severity" />
            <div><b>{{ alert.message }}</b><small>{{ alert.deviceName }} · {{ alert.resource }} · 接受于 {{ dateTime(alert.acceptedAt) }}</small><em>{{ alert.acceptedUntil ? `有效至 ${dateTime(alert.acceptedUntil)}` : '永久接受' }} · 最近观测 {{ ago(alert.lastSeenAt) }}</em></div>
            <button class="secondary-button tiny" @click="cancelAccepted(alert)">取消接受</button>
          </article>
        </div>
        <div v-else class="inline-empty">尚未接受任何风险。可在“告警”详情中选择接受期限。</div>
      </section>
    </template>
  </PageState>
</template>
