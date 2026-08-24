<script setup lang="ts">
import type { Alert } from '@/types'
import { ago, formatMetricValue } from '@/utils'
import StatusPill from './StatusPill.vue'

defineProps<{ alert: Alert; actionable?: boolean; actionLoading?: boolean }>()
const emit = defineEmits<{ action: [fingerprint: string, action: string] }>()
const states: Record<string, string> = { firing: '触发中', acknowledged: '已确认', silenced: '已静默', resolved: '已恢复' }
</script>

<template>
  <div class="alert-row" :class="`alert-${alert.severity}`">
    <div class="alert-severity"><StatusPill :status="alert.severity" /></div>
    <div class="alert-main">
      <div class="alert-title">{{ alert.deviceName || '未知设备' }}<span>{{ alert.resource || '未知资源' }}</span></div>
      <p>{{ alert.message || '告警原因未提供' }}</p>
      <div class="alert-meta">
        <span>{{ states[alert.status] || alert.status || '触发中' }}</span>
        <span>最近 {{ ago(alert.lastSeenAt || alert.observedAt || alert.collectedAt) }}</span>
        <span v-if="alert.unit">当前 {{ formatMetricValue(alert.value, alert.unit) }}</span>
      </div>
      <div class="contract-note">证据：{{ alert.message }}<template v-if="alert.unit"> · {{ formatMetricValue(alert.value, alert.unit) }}</template> · {{ ago(alert.lastSeenAt || alert.observedAt || alert.collectedAt) }}</div>
      <span v-if="actionable && alert.status !== 'resolved'" class="alert-actions">
        <button class="tiny secondary-button" :disabled="actionLoading" @click="emit('action', alert.fingerprint, 'acknowledge')">{{ actionLoading ? '处理中…' : '确认告警' }}</button>
        <button class="tiny secondary-button" :disabled="actionLoading" @click="emit('action', alert.fingerprint, 'silence')">静默 24 小时</button>
      </span>
    </div>
  </div>
</template>
