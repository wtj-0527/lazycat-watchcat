<script setup lang="ts">
import { computed } from 'vue'
export interface DonutItem { label: string; value: number; color: string }
const props = defineProps<{ items: DonutItem[]; centerLabel: string }>()
const total = computed(() => props.items.reduce((sum, item) => sum + item.value, 0))
const segments = computed(() => { let offset = 0; return props.items.map((item) => { const length = total.value ? item.value / total.value * 100 : 0; const result = { ...item, length, offset: -offset }; offset += length; return result }) })
</script>
<template>
  <div class="donut-chart">
    <svg viewBox="0 0 120 120" role="img" aria-label="状态占比图">
      <circle class="donut-track" cx="60" cy="60" r="45" />
      <circle v-for="item in segments" :key="item.label" class="donut-segment" cx="60" cy="60" r="45" :stroke="item.color" :stroke-dasharray="`${item.length} ${100 - item.length}`" :stroke-dashoffset="item.offset" pathLength="100"><title>{{ item.label }} {{ item.value }}</title></circle>
      <text x="60" y="56" text-anchor="middle" class="donut-total">{{ total }}</text><text x="60" y="73" text-anchor="middle" class="donut-label">{{ centerLabel }}</text>
    </svg>
    <div class="donut-legend"><span v-for="item in items" :key="item.label"><i :style="{ background: item.color }" />{{ item.label }} <b>{{ item.value }}</b></span></div>
  </div>
</template>
