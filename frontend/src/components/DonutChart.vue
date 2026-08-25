<script setup lang="ts">
import { computed, ref } from 'vue'
export interface DonutItem { label: string; value: number; color: string }
interface DonutSegment extends DonutItem { length: number; offset: number }
const props = defineProps<{ items: DonutItem[]; centerLabel: string }>()
const total = computed(() => props.items.reduce((sum, item) => sum + item.value, 0))
const segments = computed(() => { let offset = 0; return props.items.map((item) => { const length = total.value ? item.value / total.value * 100 : 0; const result = { ...item, length, offset: -offset }; offset += length; return result }) })
const visual = ref<HTMLDivElement>()
const active = ref<DonutSegment>()
const tooltipPosition = ref({ x: 90, y: 38 })
function percentage(item: DonutItem) {
  if (!total.value) return '0%'
  const value = item.value / total.value * 100
  return `${value.toFixed(value >= 10 ? 1 : 2)}%`
}
function showAtPointer(item: DonutSegment, event: MouseEvent) {
  active.value = item
  const rect = visual.value?.getBoundingClientRect()
  if (!rect) return
  tooltipPosition.value = {
    x: Math.max(54, Math.min(rect.width - 54, event.clientX - rect.left)),
    y: Math.max(18, Math.min(rect.height - 12, event.clientY - rect.top)),
  }
}
function showAtKeyboard(item: DonutSegment) {
  active.value = item
  tooltipPosition.value = { x: 90, y: 32 }
}
</script>
<template>
  <div class="donut-chart">
    <div ref="visual" class="donut-visual" @mouseleave="active = undefined">
      <svg viewBox="0 0 120 120" role="img" aria-label="状态占比图">
        <circle class="donut-track" cx="60" cy="60" r="45" />
        <circle
          v-for="item in segments"
          :key="item.label"
          class="donut-segment"
          :class="{ active: active?.label === item.label }"
          cx="60"
          cy="60"
          r="45"
          :stroke="item.color"
          :stroke-dasharray="`${item.length} ${100 - item.length}`"
          :stroke-dashoffset="item.offset"
          pathLength="100"
          tabindex="0"
          :aria-label="`${item.label}：${item.value}，占比 ${percentage(item)}`"
          @mouseenter="showAtPointer(item, $event)"
          @mousemove="showAtPointer(item, $event)"
          @focus="showAtKeyboard(item)"
          @blur="active = undefined"
        />
        <text x="60" y="56" text-anchor="middle" class="donut-total">{{ total }}</text><text x="60" y="73" text-anchor="middle" class="donut-label">{{ centerLabel }}</text>
      </svg>
      <div
        v-if="active"
        class="donut-tooltip"
        role="tooltip"
        :style="{ left: `${tooltipPosition.x}px`, top: `${tooltipPosition.y}px` }"
      >
        <b><i :style="{ background: active.color }" />{{ active.label }}</b>
        <span>{{ active.value }} {{ centerLabel }} · {{ percentage(active) }}</span>
      </div>
    </div>
    <div class="donut-legend">
      <span
        v-for="item in segments"
        :key="item.label"
        @mouseenter="showAtKeyboard(item)"
        @mouseleave="active = undefined"
      ><i :style="{ background: item.color }" />{{ item.label }} <b>{{ item.value }}</b></span>
    </div>
  </div>
</template>
