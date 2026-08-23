<script setup lang="ts">
import type { Alert } from '@/types'
import { ago, formatNumber } from '@/utils'
import StatusPill from './StatusPill.vue'

defineProps<{ alert: Alert; actionable?: boolean }>()
const emit = defineEmits<{ action: [fingerprint: string, action: string] }>()
const states: Record<string, string> = { firing: '触发中', acknowledged: '已确认', silenced: '已静默', resolved: '已恢复' }
</script>

<template>
  <div class="risk">
    <b>
      <span><StatusPill :status="alert.severity" /> {{ alert.deviceName }} · {{ alert.resource }}</span>
      <span>{{ states[alert.status] || alert.status }} · {{ ago(alert.lastSeenAt || alert.observedAt || alert.collectedAt) }}</span>
    </b>
    <p>{{ alert.message }}<template v-if="alert.unit"> · {{ formatNumber(alert.value) }}{{ alert.unit }}</template></p>
    <span v-if="actionable && alert.status !== 'resolved'" class="alert-actions">
      <button class="tiny ghost" @click="emit('action', alert.fingerprint, 'acknowledge')">确认</button>
      <button class="tiny ghost" @click="emit('action', alert.fingerprint, 'silence')">静默 24h</button>
      <button class="tiny ghost" @click="emit('action', alert.fingerprint, 'resolve')">解决</button>
    </span>
  </div>
</template>
