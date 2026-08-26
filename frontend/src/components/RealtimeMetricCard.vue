<script setup lang="ts">
import { ref } from 'vue'

defineProps<{
  label: string
  value: string
  detail: string
  parts?: Array<{ label: string; value: string }>
  percent?: number
  status?: 'warning' | 'critical'
  tooltipPlacement?: 'above' | 'below'
}>()

const active = ref(false)
</script>

<template>
  <div
    class="realtime-metric"
    :class="[status, { active }]"
    tabindex="0"
    :aria-label="`${label}：${value}`"
    @mouseenter="active = true"
    @mouseleave="active = false"
    @focus="active = true"
    @blur="active = false"
  >
    <span>{{ label }}</span>
    <div v-if="parts?.length" class="realtime-metric-parts">
      <span v-for="part in parts" :key="part.label"><small>{{ part.label }}</small><b>{{ part.value }}</b></span>
    </div>
    <strong v-else>{{ value }}</strong>
    <i v-if="percent !== undefined"><em :style="{ width: `${percent}%` }" /></i>
    <small class="realtime-metric-source">{{ detail }}</small>
    <div v-if="active" class="metric-hover-tooltip" :class="tooltipPlacement || 'above'" role="tooltip">
      <b>{{ label }}</b>
      <div v-if="parts?.length">
        <span v-for="part in parts" :key="part.label">{{ part.label }} <strong>{{ part.value }}</strong></span>
      </div>
      <strong v-else>{{ value }}</strong>
      <p>{{ detail }}</p>
    </div>
  </div>
</template>
