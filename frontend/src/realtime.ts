import { computed, ref } from 'vue'

export const globalRealtime = ref(false)
export const globalPollingInterval = computed(() => globalRealtime.value ? 5_000 : 30_000)

let autoStopTimer: number | undefined

export function setGlobalRealtime(enabled: boolean) {
  globalRealtime.value = enabled
  window.clearTimeout(autoStopTimer)
  if (enabled) {
    autoStopTimer = window.setTimeout(() => {
      globalRealtime.value = false
    }, 10 * 60_000)
  }
}

export function toggleGlobalRealtime() {
  setGlobalRealtime(!globalRealtime.value)
}
