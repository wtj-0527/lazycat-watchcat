import { onBeforeUnmount, onMounted, ref } from 'vue'

export function usePolling<T>(loader: () => Promise<T>, interval = 30_000) {
  const data = ref<T>()
  const loading = ref(true)
  const error = ref('')
  let timer: number | undefined

  async function refresh() {
    try {
      error.value = ''
      data.value = await loader()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      loading.value = false
    }
  }

  onMounted(() => {
    void refresh()
    timer = window.setInterval(refresh, interval)
  })
  onBeforeUnmount(() => window.clearInterval(timer))
  return { data, loading, error, refresh }
}
