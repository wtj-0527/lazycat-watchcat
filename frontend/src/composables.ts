import { computed, onBeforeUnmount, onMounted, ref, toValue, watch } from 'vue'
import type { ComputedRef, MaybeRefOrGetter, Ref } from 'vue'
import { globalPollingInterval } from '@/realtime'

export function usePolling<T>(loader: () => Promise<T>, interval: MaybeRefOrGetter<number> = globalPollingInterval) {
  const data = ref<T>()
  const loading = ref(true)
  const error = ref('')
  let timer: number | undefined
  let latestRequest = 0
  let stopped = false
  let polling = false

  async function refresh(): Promise<T | undefined> {
    const request = ++latestRequest
    try {
      const result = await loader()
      if (request !== latestRequest) return undefined
      error.value = ''
      data.value = result
      return result
    } catch (reason) {
      if (request === latestRequest) error.value = reason instanceof Error ? reason.message : String(reason)
      return undefined
    } finally {
      if (request === latestRequest) loading.value = false
    }
  }

  function schedule() {
    if (stopped || document.hidden) return
    window.clearTimeout(timer)
    timer = window.setTimeout(() => {
      polling = true
      void refresh().finally(() => {
        polling = false
        schedule()
      })
    }, Math.max(1_000, Number(toValue(interval)) || 30_000))
  }

  function handleVisibilityChange() {
    window.clearTimeout(timer)
    timer = undefined
    if (!document.hidden && !polling) {
      polling = true
      void refresh().finally(() => {
        polling = false
        schedule()
      })
    }
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleVisibilityChange)
    polling = true
    void refresh().finally(() => {
      polling = false
      schedule()
    })
  })
  watch(() => toValue(interval), () => {
    if (stopped || polling || document.hidden) return
    schedule()
  })
  onBeforeUnmount(() => {
    stopped = true
    window.clearTimeout(timer)
    document.removeEventListener('visibilitychange', handleVisibilityChange)
  })
  return { data, loading, error, refresh }
}

export function useRovingTabs<T extends string>(items: Array<[T, string]>, initial: T, idPrefix: string) {
  const selected = ref(initial) as Ref<T>

  function select(next: T) {
    selected.value = next
    requestAnimationFrame(() => document.getElementById(`${idPrefix}${next}`)?.focus())
  }

  function move(event: KeyboardEvent, current: T) {
    const index = items.findIndex(([key]) => key === current)
    let next = index
    if (event.key === 'ArrowRight') next = (index + 1) % items.length
    else if (event.key === 'ArrowLeft') next = (index - 1 + items.length) % items.length
    else if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = items.length - 1
    else return
    event.preventDefault()
    select(items[next][0])
  }

  return { selected, select, move }
}

export function usePagination<T>(items: MaybeRefOrGetter<readonly T[]>, defaultPageSize = 20) {
  const page = ref(1)
  const pageSize = ref(defaultPageSize)
  const source = computed(() => toValue(items))
  const total = computed(() => source.value.length)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
  const rangeStart = computed(() => total.value ? (page.value - 1) * pageSize.value + 1 : 0)
  const rangeEnd = computed(() => Math.min(page.value * pageSize.value, total.value))
  const pagedItems = computed(() => source.value.slice(rangeStart.value ? rangeStart.value - 1 : 0, rangeEnd.value)) as ComputedRef<T[]>

  function resetPage() {
    page.value = 1
  }

  watch([total, pageSize], () => {
    page.value = Math.min(Math.max(1, page.value), pageCount.value)
  }, { flush: 'sync' })

  return { page, pageSize, pageCount, pagedItems, rangeStart, rangeEnd, total, resetPage }
}
