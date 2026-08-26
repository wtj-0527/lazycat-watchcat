<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string; label?: string }>()
const label = computed(() => ({
  critical: '严重',
  warning: '警告',
  healthy: '健康',
  unknown: '未知',
  online: '在线',
  stale: '陈旧',
  offline: '离线',
  revoked: '已撤销',
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
  <span class="pill" :class="status">{{ props.label || label }}</span>
</template>
