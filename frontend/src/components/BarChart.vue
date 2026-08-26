<script setup lang="ts">
import { computed, ref } from 'vue'
export interface BarItem { label: string; value: number; color?: string; hint?: string }
const props = withDefaults(defineProps<{ items: BarItem[]; unit?: string }>(), { unit: '' })
const max = computed(() => Math.max(1, ...props.items.map((item) => item.value)))
const activeLabel = ref('')
</script>
<template>
  <div v-if="items.length" class="bar-chart" role="img" aria-label="分布图">
    <div
      v-for="item in items"
      :key="item.label"
      class="bar-chart-row"
      :class="{ active: activeLabel === item.label }"
      tabindex="0"
      :aria-label="`${item.label}：${item.value}${unit}`"
      @mouseenter="activeLabel = item.label"
      @mouseleave="activeLabel = ''"
      @focus="activeLabel = item.label"
      @blur="activeLabel = ''"
    >
      <span>{{ item.label }}</span><i><em :style="{ width: `${item.value / max * 100}%`, background: item.color || '#2563eb' }" /></i><b>{{ item.value }}{{ unit }}</b>
      <div v-if="activeLabel === item.label" class="bar-chart-tooltip" role="tooltip">
        <strong>{{ item.label }}</strong>
        <span>{{ item.value }}{{ unit }}</span>
        <small v-if="item.hint">{{ item.hint }}</small>
      </div>
    </div>
  </div>
  <div v-else class="inline-empty">暂无可展示的数据。</div>
</template>
