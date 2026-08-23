<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()
const label = computed(() => ({
  critical: 'Critical',
  warning: 'Warning',
  healthy: 'Healthy',
  offline: 'Offline',
  revoked: 'Revoked',
} as Record<string, string>)[props.status] || props.status)
</script>

<template>
  <span class="pill" :class="status">{{ label }}</span>
</template>
