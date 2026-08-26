<script setup lang="ts">
import { computed, ref } from 'vue'
import { bytes, formatNumber } from '@/utils'

export interface ResourceBubbleItem {
  id: string
  label: string
  detail: string
  cpu: number
  memory: number
  network: number
  io: number
  running: boolean
}

const props = defineProps<{ items: ResourceBubbleItem[] }>()
const width = 760
const height = 330
const padding = { left: 58, right: 24, top: 28, bottom: 42 }
const visual = ref<HTMLDivElement>()
const active = ref<ResourceBubbleItem>()
const tooltip = ref({ x: 0, y: 0 })
const maxCPU = computed(() => Math.max(100, ...props.items.map((item) => item.cpu)))
const maxMemory = computed(() => Math.max(1, ...props.items.map((item) => item.memory)))
const maxTraffic = computed(() => Math.max(1, ...props.items.map((item) => item.network + item.io)))
const cpuTicks = computed(() => [0, .25, .5, .75, 1].map((ratio) => ({ ratio, label: `${formatNumber(maxCPU.value * ratio, 0)}%` })))
const x = (item: ResourceBubbleItem) => padding.left + item.cpu / maxCPU.value * (width - padding.left - padding.right)
const y = (item: ResourceBubbleItem) => height - padding.bottom - item.memory / maxMemory.value * (height - padding.top - padding.bottom)
const radius = (item: ResourceBubbleItem) => 7 + Math.sqrt((item.network + item.io) / maxTraffic.value) * 17
const memoryTicks = computed(() => [0, .25, .5, .75, 1].map((ratio) => ({
  ratio,
  y: height - padding.bottom - ratio * (height - padding.top - padding.bottom),
  label: bytes(maxMemory.value * ratio),
})))

function show(item: ResourceBubbleItem, event?: MouseEvent) {
  active.value = item
  if (!event) {
    tooltip.value = { x: Math.min(width - 190, x(item) + 12), y: Math.max(12, y(item) - 34) }
    return
  }
  const rect = visual.value?.getBoundingClientRect()
  if (!rect) return
  tooltip.value = {
    x: Math.max(8, Math.min(rect.width - 286, event.clientX - rect.left + 12)),
    y: Math.max(8, Math.min(rect.height - 112, event.clientY - rect.top + 12)),
  }
}
</script>

<template>
  <div v-if="items.length" ref="visual" class="resource-bubble-chart" @mouseleave="active = undefined">
    <svg :viewBox="`0 0 ${width} ${height}`" role="img" aria-label="应用 CPU 与内存资源分布">
      <g class="bubble-grid">
        <line v-for="tick in cpuTicks" :key="`x-${tick.ratio}`" :x1="padding.left + tick.ratio * (width - padding.left - padding.right)" :x2="padding.left + tick.ratio * (width - padding.left - padding.right)" :y1="padding.top" :y2="height - padding.bottom" />
        <line v-for="tick in memoryTicks" :key="`y-${tick.ratio}`" :x1="padding.left" :x2="width - padding.right" :y1="tick.y" :y2="tick.y" />
      </g>
      <g class="bubble-axes">
        <text v-for="tick in cpuTicks" :key="`xl-${tick.ratio}`" :x="padding.left + tick.ratio * (width - padding.left - padding.right)" :y="height - 15" text-anchor="middle">{{ tick.label }}</text>
        <text v-for="tick in memoryTicks" :key="`yl-${tick.ratio}`" :x="padding.left - 9" :y="tick.y + 4" text-anchor="end">{{ tick.label }}</text>
        <text :x="width - padding.right" :y="height - 15" text-anchor="end" class="axis-title">CPU</text>
        <text :x="padding.left" y="16" class="axis-title">内存</text>
      </g>
      <circle
        v-for="item in items"
        :key="item.id"
        class="resource-bubble"
        :class="{ active: active?.id === item.id, stopped: !item.running }"
        :cx="x(item)"
        :cy="y(item)"
        :r="radius(item)"
        tabindex="0"
        :aria-label="`${item.label}，CPU ${formatNumber(item.cpu)}%，内存 ${bytes(item.memory)}`"
        @mouseenter="show(item, $event)"
        @mousemove="show(item, $event)"
        @focus="show(item)"
        @blur="active = undefined"
      />
    </svg>
    <div v-if="active" class="resource-bubble-tooltip" role="tooltip" :style="{ left: `${tooltip.x}px`, top: `${tooltip.y}px` }">
      <b>{{ active.label }}</b><small>{{ active.detail }}</small>
      <span>CPU <strong>{{ formatNumber(active.cpu) }}%</strong></span>
      <span>内存 <strong>{{ bytes(active.memory) }}</strong></span>
      <span>网络 <strong>{{ bytes(active.network) }}</strong></span>
      <span>I/O <strong>{{ bytes(active.io) }}</strong></span>
    </div>
    <div class="bubble-legend"><span><i />运行中</span><span><i class="stopped" />已停止</span><small>气泡大小表示网络与磁盘 I/O 总量</small></div>
  </div>
  <div v-else class="inline-empty">暂无容器资源指标。</div>
</template>
