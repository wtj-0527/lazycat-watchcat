<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()
const label = computed(() => ({
  critical: 'Critical',
  warning: 'Warning',
  healthy: 'Healthy',
  unknown: 'Unknown',
  online: 'Online',
  stale: 'Stale',
  offline: 'Offline',
  revoked: 'Revoked',
  firing: '触发中',
  acknowledged: '已确认',
  silenced: '已静默',
  resolved: '已恢复',
  available: '可用',
  restricted: '权限受限',
  unsupported: '不支持',
  degraded: '降级',
  unavailable: '不可用',
  error: '采集错误',
} as Record<string, string>)[props.status] || props.status)
</script>

<template>
  <span class="pill" :class="status">{{ label }}</span>
</template>
