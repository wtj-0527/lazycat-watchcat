<script setup lang="ts">
import { computed } from 'vue'
export interface BarItem { label: string; value: number; color?: string; hint?: string }
const props = withDefaults(defineProps<{ items: BarItem[]; unit?: string }>(), { unit: '' })
const max = computed(() => Math.max(1, ...props.items.map((item) => item.value)))
</script>
<template>
  <div v-if="items.length" class="bar-chart" role="img" aria-label="分布图">
    <div v-for="item in items" :key="item.label" class="bar-chart-row" :title="item.hint">
      <span>{{ item.label }}</span><i><em :style="{ width: `${item.value / max * 100}%`, background: item.color || '#2563eb' }" /></i><b>{{ item.value }}{{ unit }}</b>
    </div>
  </div>
  <div v-else class="inline-empty">暂无可展示的数据。</div>
</template>
