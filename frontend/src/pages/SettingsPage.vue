<script setup lang="ts">
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Backup, Capability, Stability } from '@/types'
import { ago, backupType, bytes, duration } from '@/utils'
import PageState from '@/components/PageState.vue'

interface Settings {
  appVersion: string
  deploymentMode: string
  embeddedCollector: boolean
  singleUser: boolean
  collectIntervalSeconds: number
  advancedIntervalSeconds: number
  rawRetentionDays: number
  rollupRetentionDays: number
  notificationChannel: string
  notificationDelivery: string
  storageStats: { rawMetricRows: number; rollupRows: number }
}
interface Operations {
  capabilities: Capability[]
  schedule: { daily: { hour: number }; weekly: { hour: number }; timezone: string }
}
interface DatabaseStatus {
  databaseSize: number
  integrityOk: boolean
  integrityError?: string
  backupCount: number
  latestBackup?: Backup
}
interface Payload {
  settings: Settings
  operations: Operations
  database: DatabaseStatus
  backups: Backup[]
  stability: Stability
}

const emit = defineEmits<{ toast: [message: string] }>()
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const [settings, operations, database, backups, stability] = await Promise.all([
    api<Settings>('/api/v1/settings'),
    api<Operations>('/api/v1/operations'),
    api<DatabaseStatus>('/api/v1/database/status'),
    api<{ items: Backup[] }>('/api/v1/backups'),
    api<Stability>('/api/v1/stability'),
  ])
  return { settings, operations, database, backups: backups.items, stability }
})

async function createBackup() {
  try {
    await api('/api/v1/backups', { method: 'POST' })
    emit('toast', '在线备份已完成并校验')
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  }
}

async function restoreBackup(name: string) {
  if (!window.confirm('恢复将重启猫眼，并在替换前再创建安全备份。确定继续？')) return
  try {
    await api(`/api/v1/backups/${encodeURIComponent(name)}/restore`, { method: 'POST' })
    emit('toast', '恢复请求已提交，应用即将重启')
    window.setTimeout(() => location.reload(), 4_000)
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  }
}

async function resetStability() {
  if (!window.confirm('确定清零当前稳定性观测并重新计算 7 天周期？')) return
  try {
    await api('/api/v1/stability/reset', { method: 'POST' })
    emit('toast', '7 天稳定性观测已重新开始')
    await refresh()
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error">
    <div class="page-head"><div><h2>设置与运维</h2><span class="muted">生产备份恢复、7 天稳定性观测与能力透明度</span></div></div>
    <div v-if="data" class="two">
      <div>
        <div class="card">
          <h2>数据库保护</h2>
          <p :class="data.database.integrityOk ? 'green' : 'red'"><i class="status-dot" :class="{ bad: !data.database.integrityOk }" />{{ data.database.integrityOk ? 'SQLite 完整性检查通过' : data.database.integrityError }}</p>
          <div class="risk"><b>数据库大小<span>{{ bytes(data.database.databaseSize) }}</span></b></div>
          <div class="risk"><b>备份数量<span>{{ data.database.backupCount }}</span></b></div>
          <div class="risk"><b>最近备份<span>{{ data.database.latestBackup ? `${new Date(data.database.latestBackup.createdAt).toLocaleString()} · ${backupType(data.database.latestBackup.type)}` : '尚无备份' }}</span></b></div>
          <div class="button-row"><button @click="createBackup">立即备份</button></div>
          <div class="backup-list">
            <div v-for="backup in data.backups.slice(0, 8)" :key="backup.name" class="risk">
              <b>{{ backupType(backup.type) }} · v{{ backup.appVersion }}<span :class="backup.verified ? 'green' : 'red'">{{ backup.verified ? '已校验' : '校验失败' }}</span></b>
              <p>{{ new Date(backup.createdAt).toLocaleString() }} · {{ bytes(backup.size) }} · SHA-256 {{ backup.sha256.slice(0, 12) }}…</p>
              <button class="tiny danger" :disabled="!backup.verified" @click="restoreBackup(backup.name)">恢复此备份</button>
            </div>
            <p v-if="!data.backups.length" class="muted">尚无备份。版本升级时会自动创建升级前备份。</p>
          </div>
        </div>
        <div class="card card-spaced">
          <h2>7 天长期稳定性观测</h2>
          <div class="risk"><b>开始时间<span>{{ new Date(data.stability.startedAt).toLocaleString() }}</span></b></div>
          <div class="risk"><b>目标完成<span>{{ new Date(data.stability.targetEndAt).toLocaleString() }}</span></b></div>
          <div class="risk"><b>采样次数<span>{{ data.stability.sampleCount }}</span></b></div>
          <div class="risk"><b>失败次数<span>{{ data.stability.failureCount }}</span></b></div>
          <div class="risk"><b>连续失败<span>{{ data.stability.consecutiveFailures }}</span></b></div>
          <div class="risk"><b>数据库延迟<span>{{ data.stability.databaseLatencyMs }} ms</span></b></div>
          <div class="risk"><b>指标新鲜度<span>{{ data.stability.metricFreshnessSeconds == null ? '尚无指标' : `${data.stability.metricFreshnessSeconds} 秒` }}</span></b></div>
          <div class="risk"><b>待发送通知<span>{{ data.stability.pendingNotifications }}</span></b></div>
          <p :class="data.stability.qualified ? 'green' : 'amber'">{{ data.stability.qualified ? '已满足连续 7 天无失败资格' : `观测进行中，剩余约 ${duration(data.stability.remainingSeconds)}` }}</p>
          <button class="ghost" @click="resetStability">重新开始 7 天观测</button>
        </div>
        <div class="card card-spaced">
          <h2>采集能力校准</h2>
          <div v-for="capability in data.operations.capabilities" :key="capability.capability" class="risk">
            <b>{{ capability.capability }}<span :class="capability.status === 'available' ? 'green' : capability.status === 'degraded' ? 'amber' : 'muted'">{{ { available: '可用', degraded: '降级', unavailable: '不可用' }[capability.status] || capability.status }}</span></b>
            <p>{{ capability.detail }} · 检查于 {{ ago(capability.checkedAt) }}</p>
          </div>
        </div>
      </div>
      <div>
        <div class="card">
          <h2>生产参数</h2>
          <div class="risk"><b>应用版本<span>v{{ data.settings.appVersion }}</span></b></div>
          <div class="risk"><b>部署模式<span>{{ data.settings.deploymentMode === 'single-lpk' ? '单一 LPK' : '未知' }}</span></b></div>
          <div class="risk"><b>本机采集<span>{{ data.settings.embeddedCollector ? '已内置' : '未启用' }}</span></b></div>
          <div class="risk"><b>用户模式<span>{{ data.settings.singleUser ? '单用户' : '多用户' }}</span></b></div>
          <div class="risk"><b>基础采集<span>{{ data.settings.collectIntervalSeconds }} 秒</span></b></div>
          <div class="risk"><b>高级采集<span>{{ data.settings.advancedIntervalSeconds }} 秒</span></b></div>
          <div class="risk"><b>原始数据<span>{{ data.settings.rawRetentionDays }} 天</span></b></div>
          <div class="risk"><b>降采样数据<span>{{ data.settings.rollupRetentionDays }} 天</span></b></div>
          <div class="risk"><b>通知渠道<span>{{ data.settings.notificationChannel === 'lazycat' ? 'LazyCat 系统通知' : data.settings.notificationChannel }}</span></b></div>
          <div class="risk"><b>通知投递<span>{{ data.settings.notificationDelivery === 'outbox-retry' ? '持久队列重试' : data.settings.notificationDelivery }}</span></b></div>
          <div class="risk"><b>原始指标行<span>{{ data.settings.storageStats.rawMetricRows }}</span></b></div>
          <div class="risk"><b>小时降采样行<span>{{ data.settings.storageStats.rollupRows }}</span></b></div>
        </div>
        <div class="card card-spaced">
          <h2>单 LPK 部署</h2><p class="green">本机 Collector 已内置，仅安装一个猫眼 LPK。</p>
          <div class="risk"><b>每日巡检<span>{{ data.operations.schedule.daily.hour }}:00</span></b></div>
          <div class="risk"><b>每周巡检<span>周日 {{ data.operations.schedule.weekly.hour }}:00</span></b></div>
          <div class="risk"><b>时区<span>{{ data.operations.schedule.timezone }}</span></b></div>
        </div>
      </div>
    </div>
  </PageState>
</template>
