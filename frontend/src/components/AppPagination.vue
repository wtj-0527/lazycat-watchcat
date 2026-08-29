<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  total: number
  pageCount: number
  rangeStart: number
  rangeEnd: number
  page: number
  pageSize: number
  pageSizes?: number[]
  label?: string
}>(), {
  pageSizes: () => [10, 20, 50],
  label: '列表分页',
})
const emit = defineEmits<{
  'update:page': [value: number]
  'update:pageSize': [value: number]
}>()

const effectivePageSizes = computed(() =>
  [...new Set([...props.pageSizes, props.pageSize])].filter(size => size > 0).sort((a, b) => a - b),
)

const visiblePages = computed(() => {
  if (props.pageCount <= 7) return Array.from({ length: props.pageCount }, (_, index) => index + 1)
  const candidates = new Set([1, props.pageCount, props.page - 1, props.page, props.page + 1])
  const values = [...candidates].filter((value) => value >= 1 && value <= props.pageCount).sort((a, b) => a - b)
  const result: Array<number | string> = []
  values.forEach((value, index) => {
    if (index && value - values[index - 1] > 1) result.push(`ellipsis-${value}`)
    result.push(value)
  })
  return result
})

function setPage(next: number) {
  emit('update:page', Math.min(Math.max(1, next), props.pageCount))
}

function setPageSize(event: Event) {
  emit('update:pageSize', Number((event.target as HTMLSelectElement).value))
  emit('update:page', 1)
}
</script>

<template>
  <nav v-if="total > pageSize" class="app-pagination" :aria-label="label">
    <span class="pagination-range">第 {{ rangeStart }}–{{ rangeEnd }} 条，共 {{ total }} 条</span>
    <label class="pagination-size">
      <span>每页</span>
      <select :value="pageSize" :aria-label="`${label}每页条数`" @change="setPageSize">
        <option v-for="size in effectivePageSizes" :key="size" :value="size">{{ size }} 条</option>
      </select>
    </label>
    <div class="pagination-pages">
      <button type="button" :disabled="page <= 1" :aria-label="`${label}上一页`" @click="setPage(page - 1)">‹</button>
      <template v-for="item in visiblePages" :key="item">
        <span v-if="typeof item === 'string'" class="pagination-ellipsis" aria-hidden="true">…</span>
        <button
          v-else
          type="button"
          :class="{ active: item === page }"
          :aria-label="`${label}第 ${item} 页`"
          :aria-current="item === page ? 'page' : undefined"
          @click="setPage(item)"
        >{{ item }}</button>
      </template>
      <button type="button" :disabled="page >= pageCount" :aria-label="`${label}下一页`" @click="setPage(page + 1)">›</button>
    </div>
  </nav>
</template>
