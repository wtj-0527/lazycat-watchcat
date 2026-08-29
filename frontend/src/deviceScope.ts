import { ref } from 'vue'

const STORAGE_KEY = 'watchcatDeviceScope'
const stored = typeof localStorage === 'undefined' ? 'all' : localStorage.getItem(STORAGE_KEY) || 'all'

export const globalDeviceId = ref(stored)

export function selectGlobalDevice(deviceId: string) {
  globalDeviceId.value = deviceId || 'all'
  if (typeof localStorage !== 'undefined') localStorage.setItem(STORAGE_KEY, globalDeviceId.value)
}
