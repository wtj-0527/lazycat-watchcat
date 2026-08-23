import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { Ref } from 'vue'

export function usePolling<T>(loader: () => Promise<T>, interval = 30_000) {
  const data = ref<T>()
  const loading = ref(true)
  const error = ref('')
  let timer: number | undefined
  let latestRequest = 0
  let stopped = false

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
    if (stopped) return
    timer = window.setTimeout(() => {
      void refresh().finally(schedule)
    }, interval)
  }

  onMounted(() => {
    void refresh().finally(schedule)
  })
  onBeforeUnmount(() => {
    stopped = true
    window.clearTimeout(timer)
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
