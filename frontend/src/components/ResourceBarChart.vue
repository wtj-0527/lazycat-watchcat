<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppPagination from '@/components/AppPagination.vue'
import { bytes, formatNumber } from '@/utils'

export interface ResourceBarItem {
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

const props = defineProps<{ items: ResourceBarItem[] }>()
const chart = ref<HTMLDivElement>()
const metric = ref<MetricKey>('cpu')
const page = ref(1)
const pageSize = ref(10)
const active = ref<ResourceBarItem>()
const tooltip = ref({ x: 0, y: 0 })
const metrics: MetricDefinition[] = [
  { key: 'cpu', label: 'CPU', caption: '当前使用率', color: '#2563eb' },
  { key: 'memory', label: '内存', caption: '当前占用', color: '#7c3aed' },
  { key: 'network', label: '网络', caption: '累计收发', color: '#118847' },
  { key: 'io', label: '磁盘 I/O', caption: '累计读写', color: '#c05600' },
]
const currentMetric = computed(() => metrics.find((item) => item.key === metric.value) || metrics[0])
const running = computed(() => props.items.filter((item) => item.running).length)
const stopped = computed(() => props.items.length - running.value)
const totalMemory = computed(() => props.items.reduce((sum, item) => sum + item.memory, 0))
const totalNetwork = computed(() => props.items.reduce((sum, item) => sum + item.network, 0))
const totalIO = computed(() => props.items.reduce((sum, item) => sum + item.io, 0))
const sorted = computed(() => [...props.items].sort((a, b) =>
  value(b) - value(a) || a.label.localeCompare(b.label) || a.detail.localeCompare(b.detail)))
const maximum = computed(() => Math.max(1, ...sorted.value.map(value)))
const pageCount = computed(() => Math.max(1, Math.ceil(sorted.value.length / pageSize.value)))
const rangeStart = computed(() => sorted.value.length ? (page.value - 1) * pageSize.value + 1 : 0)
const rangeEnd = computed(() => Math.min(sorted.value.length, page.value * pageSize.value))
const visible = computed(() => sorted.value.slice(rangeStart.value - 1, rangeEnd.value))
watch(metric, () => { page.value = 1 })
watch(pageSize, () => { page.value = 1 })
watch(pageCount, (count) => { page.value = Math.min(page.value, count) })

function value(item: ResourceBarItem) {
  return item[metric.value]
}
function display(item: ResourceBarItem, key: MetricKey = metric.value) {
  return key === 'cpu' ? `${formatNumber(item.cpu)}%` : bytes(item[key])
}
function barWidth(item: ResourceBarItem) {
  const raw = value(item)
  const normalized = metric.value === 'network' || metric.value === 'io'
    ? Math.log1p(raw) / Math.log1p(maximum.value)
    : raw / maximum.value
  return `${raw > 0 ? Math.max(1.5, normalized * 100) : 0}%`
}
function axisLabel(ratio: number) {
  const amount = maximum.value * ratio
  return metric.value === 'cpu' ? `${formatNumber(amount, 0)}%` : bytes(amount)
}
function selectMetric(key: MetricKey) {
  metric.value = key
  active.value = undefined
}
function show(item: ResourceBarItem, event?: MouseEvent) {
  active.value = item
  const rect = chart.value?.getBoundingClientRect()
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
  <div v-if="items.length" ref="chart" class="resource-bar-explorer" @mouseleave="active = undefined">
    <div class="resource-summary-strip">
      <article><span>实例</span><strong>{{ items.length }}</strong><small>全部已采集实例</small></article>
      <article><span>运行状态</span><strong class="status-total"><i />{{ running }}<em v-if="stopped">· {{ stopped }} 已停止</em></strong><small>{{ stopped ? '包含已停止实例' : '全部运行中' }}</small></article>
      <article><span>内存合计</span><strong>{{ bytes(totalMemory) }}</strong><small>当前容器占用</small></article>
      <article><span>累计吞吐</span><strong>{{ bytes(totalNetwork + totalIO) }}</strong><small>网络 {{ bytes(totalNetwork) }} · I/O {{ bytes(totalIO) }}</small></article>
    </div>

    <div class="resource-chart-toolbar">
      <div class="resource-metric-tabs" role="tablist" aria-label="资源指标">
        <button
          v-for="item in metrics"
          :key="item.key"
          type="button"
          role="tab"
          :aria-selected="metric === item.key"
          :class="{ active: metric === item.key }"
          @click="selectMetric(item.key)"
        ><i :style="{ background: item.color }" />{{ item.label }}</button>
      </div>
      <span>{{ currentMetric.caption }} · 从高到低</span>
    </div>

    <div class="resource-horizontal-chart" :style="{ '--resource-color': currentMetric.color }">
      <div class="resource-chart-axis" aria-hidden="true">
        <span v-for="ratio in [0, .25, .5, .75, 1]" :key="ratio" :style="{ left: `${ratio * 100}%` }">{{ axisLabel(ratio) }}</span>
      </div>
      <div class="resource-chart-rows">
        <div
          v-for="(item, index) in visible"
          :key="item.id"
          class="resource-chart-row"
          :class="{ active: active?.id === item.id }"
          tabindex="0"
          @mouseenter="show(item, $event)"
          @mousemove="show(item, $event)"
          @focus="show(item)"
          @blur="active = undefined"
        >
          <span class="resource-chart-rank">{{ rangeStart + index }}</span>
          <span class="resource-chart-identity"><b>{{ item.label }}</b><small>{{ item.detail }}</small></span>
          <span class="resource-chart-plot">
            <i v-for="ratio in [0, .25, .5, .75, 1]" :key="ratio" :style="{ left: `${ratio * 100}%` }" />
            <em :style="{ width: barWidth(item) }" />
          </span>
          <strong>{{ display(item) }}</strong>
          <span class="resource-chart-state" :class="{ stopped: !item.running }"><i />{{ item.running ? '运行中' : '已停止' }}</span>
        </div>
      </div>
    </div>

    <AppPagination
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="items.length"
      :page-count="pageCount"
      :range-start="rangeStart"
      :range-end="rangeEnd"
      :page-sizes="[10, 20, 50]"
      label="实例资源图分页"
    />

    <div v-if="active" class="resource-ranking-tooltip" role="tooltip" :style="{ left: `${tooltip.x}px`, top: `${tooltip.y}px` }">
      <b>{{ active.label }}</b>
      <small>{{ active.detail }}</small>
      <span>CPU <strong>{{ display(active, 'cpu') }}</strong></span>
      <span>内存 <strong>{{ display(active, 'memory') }}</strong></span>
      <span>网络 <strong>{{ display(active, 'network') }}</strong></span>
      <span>I/O <strong>{{ display(active, 'io') }}</strong></span>
    </div>
  </div>
  <div v-else class="inline-empty">暂无容器资源指标。</div>
</template>
