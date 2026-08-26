<script setup lang="ts">
import { computed, ref } from 'vue'
import { bytes, formatNumber } from '@/utils'

export interface ResourceRankingItem {
  id: string
  label: string
  detail: string
  cpu: number
  memory: number
  network: number
  io: number
  running: boolean
}

type MetricKey = 'cpu' | 'memory' | 'network' | 'io'
interface MetricDefinition {
  key: MetricKey
  label: string
  caption: string
  color: string
}

const props = defineProps<{ items: ResourceRankingItem[] }>()
const board = ref<HTMLDivElement>()
const active = ref<{ item: ResourceRankingItem; metric: MetricKey }>()
const tooltip = ref({ x: 0, y: 0 })
const metrics: MetricDefinition[] = [
  { key: 'cpu', label: 'CPU', caption: '当前使用率', color: '#2563eb' },
  { key: 'memory', label: '内存', caption: '当前占用', color: '#7c3aed' },
  { key: 'network', label: '网络', caption: '累计收发', color: '#118847' },
  { key: 'io', label: '磁盘 I/O', caption: '累计读写', color: '#c05600' },
]
const running = computed(() => props.items.filter((item) => item.running).length)
const stopped = computed(() => props.items.length - running.value)
const totalMemory = computed(() => props.items.reduce((sum, item) => sum + item.memory, 0))
const totalNetwork = computed(() => props.items.reduce((sum, item) => sum + item.network, 0))
const totalIO = computed(() => props.items.reduce((sum, item) => sum + item.io, 0))

function value(item: ResourceRankingItem, metric: MetricKey) {
  return item[metric]
}
function display(item: ResourceRankingItem, metric: MetricKey) {
  return metric === 'cpu' ? `${formatNumber(item.cpu)}%` : bytes(value(item, metric))
}
function leaders(metric: MetricKey) {
  return [...props.items]
    .sort((a, b) => value(b, metric) - value(a, metric) || a.label.localeCompare(b.label))
}
function maximum(metric: MetricKey) {
  return Math.max(1, ...props.items.map((item) => value(item, metric)))
}
function barWidth(item: ResourceRankingItem, metric: MetricKey) {
  const normalized = metric === 'network' || metric === 'io'
    ? Math.log1p(value(item, metric)) / Math.log1p(maximum(metric))
    : value(item, metric) / maximum(metric)
  return `${value(item, metric) > 0 ? Math.max(4, normalized * 100) : 0}%`
}
function show(item: ResourceRankingItem, metric: MetricKey, event?: MouseEvent) {
  active.value = { item, metric }
  const rect = board.value?.getBoundingClientRect()
  if (!rect) return
  const pointerX = event ? event.clientX - rect.left : rect.width / 2
  const pointerY = event ? event.clientY - rect.top : rect.height / 2
  tooltip.value = {
    x: Math.max(8, Math.min(rect.width - 294, pointerX + 12)),
    y: Math.max(8, Math.min(rect.height - 142, pointerY + 12)),
  }
}
</script>

<template>
  <div v-if="items.length" ref="board" class="resource-ranking-board" @mouseleave="active = undefined">
    <div class="resource-summary-strip">
      <article><span>实例</span><strong>{{ items.length }}</strong><small>全部已采集实例</small></article>
      <article><span>运行状态</span><strong class="status-total"><i />{{ running }}<em v-if="stopped">· {{ stopped }} 已停止</em></strong><small>{{ stopped ? '包含已停止实例' : '全部运行中' }}</small></article>
      <article><span>内存合计</span><strong>{{ bytes(totalMemory) }}</strong><small>当前容器占用</small></article>
      <article><span>累计吞吐</span><strong>{{ bytes(totalNetwork + totalIO) }}</strong><small>网络 {{ bytes(totalNetwork) }} · I/O {{ bytes(totalIO) }}</small></article>
    </div>

    <div class="resource-ranking-grid">
      <section v-for="metric in metrics" :key="metric.key" class="resource-ranking-column">
        <header>
          <span><i :style="{ background: metric.color }" />{{ metric.label }}</span>
          <small>{{ metric.caption }} · {{ items.length }} 个</small>
        </header>
        <div class="resource-ranking-list" tabindex="0" :aria-label="`${metric.label}实例排行，可上下滚动`">
          <div
            v-for="(item, index) in leaders(metric.key)"
            :key="`${metric.key}:${item.id}`"
            class="resource-ranking-row"
            :class="{ active: active?.item.id === item.id && active?.metric === metric.key }"
            tabindex="0"
            @mouseenter="show(item, metric.key, $event)"
            @mousemove="show(item, metric.key, $event)"
            @focus="show(item, metric.key)"
            @blur="active = undefined"
          >
            <span class="resource-rank">{{ index + 1 }}</span>
            <span class="resource-ranking-identity"><b>{{ item.label }}</b><small>{{ item.detail }}</small></span>
            <strong>{{ display(item, metric.key) }}</strong>
            <i class="resource-ranking-track"><em :style="{ width: barWidth(item, metric.key), background: metric.color }" /></i>
          </div>
        </div>
      </section>
    </div>

    <div v-if="active" class="resource-ranking-tooltip" role="tooltip" :style="{ left: `${tooltip.x}px`, top: `${tooltip.y}px` }">
      <b>{{ active.item.label }}</b>
      <small>{{ active.item.detail }}</small>
      <span>CPU <strong>{{ formatNumber(active.item.cpu) }}%</strong></span>
      <span>内存 <strong>{{ bytes(active.item.memory) }}</strong></span>
      <span>网络 <strong>{{ bytes(active.item.network) }}</strong></span>
      <span>I/O <strong>{{ bytes(active.item.io) }}</strong></span>
    </div>
  </div>
  <div v-else class="inline-empty">暂无容器资源指标。</div>
</template>
