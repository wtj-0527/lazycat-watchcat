<script setup lang="ts">
import { computed } from 'vue'

export interface ChartPoint { value: number; label?: string; at?: string }
export interface ChartSeries { name: string; color: string; points: ChartPoint[] }
const props = withDefaults(defineProps<{ series: ChartSeries[]; min?: number; max?: number; unit?: string; height?: number }>(), { min: 0, unit: '', height: 220 })
const width = 900
const pad = { left: 42, right: 18, top: 18, bottom: 30 }
const normalizedSeries = computed(() => props.series.map((item) => {
  if (item.points.length <= 120) return item
  const step = (item.points.length - 1) / 119
  return { ...item, points: Array.from({ length: 120 }, (_, index) => item.points[Math.round(index * step)]) }
}))
const all = computed(() => normalizedSeries.value.flatMap((item) => item.points).filter((point) => Number.isFinite(point.value)))
const upper = computed(() => props.max ?? Math.max(1, ...all.value.map((point) => point.value)))
const lower = computed(() => props.min)
function x(index: number, count: number) { return pad.left + (count <= 1 ? 0 : index / (count - 1) * (width - pad.left - pad.right)) }
function y(value: number) { const range = Math.max(1, upper.value - lower.value); return pad.top + (upper.value - value) / range * (props.height - pad.top - pad.bottom) }
function path(points: ChartPoint[]) { return points.map((point, index) => `${index ? 'L' : 'M'} ${x(index, points.length).toFixed(1)} ${y(point.value).toFixed(1)}`).join(' ') }
const ticks = computed(() => [upper.value, upper.value * .75 + lower.value * .25, (upper.value + lower.value) / 2, upper.value * .25 + lower.value * .75, lower.value])
const axisLabels = computed(() => {
  const longest = normalizedSeries.value.reduce<ChartPoint[]>((best, item) => item.points.length > best.length ? item.points : best, [])
  if (!longest.length) return []
  const indexes = [...new Set([0, Math.floor((longest.length - 1) / 2), longest.length - 1])]
  return indexes.map((index) => ({ x: x(index, longest.length), label: longest[index]?.label || longest[index]?.at || '' }))
})
</script>

<template>
  <div class="line-chart" :style="{ height: `${height}px` }">
    <svg v-if="all.length" :viewBox="`0 0 ${width} ${height}`" role="img" aria-label="历史趋势图" preserveAspectRatio="none">
      <g class="chart-grid">
        <template v-for="(tick, index) in ticks" :key="index">
          <line :x1="pad.left" :x2="width - pad.right" :y1="y(tick)" :y2="y(tick)" />
          <text x="4" :y="y(tick) + 4">{{ tick.toFixed(tick >= 10 ? 0 : 1) }}{{ unit }}</text>
        </template>
      </g>
      <g v-for="item in normalizedSeries" :key="item.name">
        <path class="chart-line" :d="path(item.points)" :stroke="item.color" />
        <circle v-for="(point, index) in item.points" :key="index" :cx="x(index, item.points.length)" :cy="y(point.value)" r="3" :fill="item.color">
          <title>{{ item.name }} · {{ point.value.toFixed(1) }}{{ unit }} · {{ point.at || point.label }}</title>
        </circle>
      </g>
      <text v-for="item in axisLabels" :key="item.x" class="chart-axis-label" :x="item.x" :y="height - 7" text-anchor="middle">{{ item.label }}</text>
    </svg>
    <div v-else class="inline-empty">当前时间范围内没有历史数据。</div>
    <div v-if="series.length" class="chart-legend"><span v-for="item in series" :key="item.name"><i :style="{ background: item.color }" />{{ item.name }}</span></div>
  </div>
</template>
