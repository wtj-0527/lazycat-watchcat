<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '@/api'
import type { Overview } from '@/types'
import { ago } from '@/utils'
import OverviewPage from '@/pages/OverviewPage.vue'
import DevicesPage from '@/pages/DevicesPage.vue'
import AppsPage from '@/pages/AppsPage.vue'
import UsersPage from '@/pages/UsersPage.vue'
import StoragePage from '@/pages/StoragePage.vue'
import AlertsPage from '@/pages/AlertsPage.vue'
import InspectionsPage from '@/pages/InspectionsPage.vue'
import SettingsPage from '@/pages/SettingsPage.vue'
import AppIcon from '@/components/AppIcon.vue'
import AppDialog from '@/components/AppDialog.vue'
import SmartSelect, { type SmartOption } from '@/components/SmartSelect.vue'
import { usePolling } from '@/composables'
import { globalDeviceId, selectGlobalDevice } from '@/deviceScope'
import { globalPollingInterval, globalRealtime, toggleGlobalRealtime } from '@/realtime'
import { applyTheme, storedTheme, type ThemeMode } from '@/theme'

type Page = 'overview' | 'devices' | 'apps' | 'users' | 'storage' | 'alerts' | 'inspections' | 'onboarding' | 'settings'
const navs: Array<[Page, string]> = [
  ['overview', '总览'], ['devices', '设备'], ['apps', '应用'], ['users', '用户'],
  ['storage', '存储'], ['alerts', '告警'], ['inspections', '巡检'],
  ['onboarding', '接入'], ['settings', '设置'],
]
const pages = {
  overview: OverviewPage,
  devices: DevicesPage,
  apps: AppsPage,
  users: UsersPage,
  storage: StoragePage,
  alerts: AlertsPage,
  inspections: InspectionsPage,
  onboarding: SettingsPage,
  settings: SettingsPage,
}

const page = ref<Page>('overview')
const version = ref('—')
const { data: fleet } = usePolling(async () => {
  const result = await api<Overview>('/api/v1/overview')
  return { ...result, devices: result.devices || [], alerts: result.alerts || [] }
}, globalPollingInterval)
const toastMessage = ref('')
const globalQuery = ref('')
const searchNonce = ref(0)
const themeMode = ref<ThemeMode>(storedTheme())
const deviceDark = ref(false)
let deviceThemeQuery: MediaQueryList | undefined
let toastTimer: number | undefined
let versionTimer: number | undefined

const pageComponent = computed(() => pages[page.value])
const pageLabel = computed(() => navs.find(([key]) => key === page.value)?.[1] || '总览')
const deviceScopedPage = computed(() => ['overview', 'devices', 'apps', 'users', 'storage', 'alerts'].includes(page.value))
const pageProps = computed(() => page.value === 'settings'
  ? { initialTab: 'thresholds' }
  : page.value === 'onboarding' ? { initialTab: 'onboarding' } : {})
const deviceCount = computed(() => fleet.value?.stats.devices ?? 0)
const deviceOptions = computed<SmartOption[]>(() => (fleet.value?.devices || []).map((device) => ({
  value: device.id,
  label: device.name,
  meta: `${device.hostname || device.id} · ${device.online && !device.stale ? '在线' : device.stale ? '数据陈旧' : '离线'}`,
})).sort((left, right) => left.label.localeCompare(right.label)))
const scopedDeviceCount = computed(() => globalDeviceId.value === 'all' ? deviceCount.value : 1)
const onlineCount = computed(() => fleet.value?.stats.online ?? 0)
const staleCount = computed(() => fleet.value?.devices.filter((device) => device.stale).length ?? 0)
const freshness = computed(() => ago(fleet.value?.updatedAt))
const activeAlerts = computed(() => (fleet.value?.alerts || []).filter((alert) =>
  alert.status !== 'resolved' && (globalDeviceId.value === 'all' || alert.deviceId === globalDeviceId.value)))
const activeAlertCount = computed(() => activeAlerts.value.length)
const hasCriticalAlert = computed(() => activeAlerts.value.some((alert) => alert.severity === 'critical'))
const themes: Array<{ mode: ThemeMode; label: string; symbol: string }> = [
  { mode: 'light', label: '白天', symbol: '☀' },
  { mode: 'dark', label: '夜晚', symbol: '☾' },
  { mode: 'device', label: '设备', symbol: '◐' },
]
const currentTheme = computed(() => themes.find((item) => item.mode === themeMode.value) || themes[2])
const nextTheme = computed(() => themes[(themes.findIndex((item) => item.mode === themeMode.value) + 1) % themes.length])

watch(deviceOptions, (options) => {
  if (globalDeviceId.value !== 'all' && !options.some((item) => item.value === globalDeviceId.value)) selectGlobalDevice('all')
})

function pageFromHash(): Page {
  const candidate = location.hash.slice(1).split('?')[0] as Page
  return candidate in pages ? candidate : 'overview'
}
function navigate(next: Page) {
  page.value = next
  if (location.hash !== `#${next}`) location.hash = next
}
function syncHash() {
  page.value = pageFromHash()
}
function toast(message: string) {
  toastMessage.value = message
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => { toastMessage.value = '' }, 2200)
}
async function checkVersion() {
  try {
    const next = await api<{ version: string }>(`/api/v1/version?ts=${Date.now()}`, { cache: 'no-store' })
    if (version.value !== '—' && version.value !== next.version) {
      window.location.reload()
      return
    }
    version.value = next.version
  } catch {
    if (version.value === '—') version.value = '—'
  }
}
function submitGlobalSearch() {
  const value = globalQuery.value.trim().toLowerCase()
  if (!value) return
  sessionStorage.setItem('watchcatSearch', globalQuery.value.trim())
  if (value.includes('告警') || value.includes('alert')) navigate('alerts')
  else if (value.includes('应用') || value.includes('app')) navigate('apps')
  else navigate('devices')
  searchNonce.value++
  toast(`正在搜索“${globalQuery.value.trim()}”`)
}
function selectTheme(mode: ThemeMode) {
  themeMode.value = mode
  localStorage.setItem('watchcatTheme', mode)
  applyTheme(mode, deviceDark.value)
}
function cycleTheme() {
  selectTheme(nextTheme.value.mode)
}
function syncDeviceTheme(event?: MediaQueryListEvent) {
  deviceDark.value = event?.matches ?? deviceThemeQuery?.matches ?? false
  if (themeMode.value === 'device') applyTheme('device', deviceDark.value)
}

onMounted(async () => {
  if (typeof window.matchMedia === 'function') {
    deviceThemeQuery = window.matchMedia('(prefers-color-scheme: dark)')
    syncDeviceTheme()
    deviceThemeQuery.addEventListener('change', syncDeviceTheme)
  }
  syncHash()
  window.addEventListener('hashchange', syncHash)
  void checkVersion()
  versionTimer = window.setInterval(checkVersion, 30_000)
})
onBeforeUnmount(() => {
  deviceThemeQuery?.removeEventListener('change', syncDeviceTheme)
  window.removeEventListener('hashchange', syncHash)
  window.clearTimeout(toastTimer)
  window.clearInterval(versionTimer)
})
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <img class="brand-logo" src="/watchcat-logo-64.png" alt="WatchCat Logo">
      <div><b>WatchCat</b><small>设备群监控</small></div>
    </div>
    <nav aria-label="主导航">
      <button v-for="[key, label] in navs" :key="key" class="nav-item" :class="{ active: page === key }" :aria-current="page === key ? 'page' : undefined" @click="navigate(key)">
        <AppIcon :name="key" /> <span>{{ label }}</span>
      </button>
    </nav>
    <div class="sidebar-footer">
      <div class="collection-status">
        <span>采集状态</span>
        <b><i /> {{ onlineCount }} 台在线 · {{ staleCount }} 台陈旧</b>
        <small>数据新鲜度 {{ freshness }}</small>
        <small>WatchCat v{{ version }}</small>
      </div>
    </div>
  </aside>
  <main class="app-main">
    <header class="topbar">
      <div class="topbar-context">
        <span class="topbar-page-name">{{ pageLabel }}</span>
        <div v-if="deviceScopedPage" class="scope-picker">
          <SmartSelect
            :model-value="globalDeviceId"
            :options="deviceOptions"
            :all-label="`全部设备 · ${deviceCount} 台`"
            control-label="全局设备筛选"
            searchable
            @update:model-value="selectGlobalDevice"
          />
        </div>
        <div v-else class="scope-picker static-scope" title="该页面使用平台全局配置或不可变报告">
          <span>平台范围</span><b>{{ scopedDeviceCount }} 台设备</b>
        </div>
      </div>
      <div class="topbar-actions">
        <span class="freshness-pill" :class="{ stale: staleCount }">更新 {{ freshness }}</span>
        <button
          class="topbar-realtime-button"
          :class="{ active: globalRealtime }"
          type="button"
          :aria-pressed="globalRealtime"
          :title="globalRealtime ? '关闭全局实时模式并恢复每 30 秒刷新' : '所有页面每 5 秒读取最新数据，10 分钟后自动关闭'"
          @click="toggleGlobalRealtime"
        ><i />{{ globalRealtime ? '实时 · 5 秒' : '实时' }}</button>
        <form class="global-search" role="search" @submit.prevent="submitGlobalSearch">
          <AppIcon name="search" :size="16" />
          <input v-model="globalQuery" aria-label="全局搜索" placeholder="搜索设备、应用、告警...">
        </form>
        <button
          class="theme-toggle"
          type="button"
          :aria-label="`当前为${currentTheme.label}主题，点击切换为${nextTheme.label}主题`"
          :title="`当前：${currentTheme.label}；点击切换为：${nextTheme.label}`"
          @click="cycleTheme"
        ><i>{{ currentTheme.symbol }}</i><span>{{ currentTheme.label }}</span></button>
        <button
          class="icon-button notification-button"
          :class="{ active: activeAlertCount > 0, critical: hasCriticalAlert }"
          type="button"
          :aria-label="activeAlertCount ? `${activeAlertCount} 个待处理告警` : '暂无待处理告警'"
          @click="navigate('alerts')"
        >
          <AppIcon name="bell" :size="18" />
          <span v-if="activeAlertCount" class="notification-badge">{{ activeAlertCount > 99 ? '99+' : activeAlertCount }}</span>
        </button>
      </div>
    </header>
    <section id="content" :class="`page-${page}`">
      <Transition name="page" mode="out-in">
        <div :key="`${page}-${searchNonce}`" class="page-view">
          <component :is="pageComponent" v-bind="pageProps" @toast="toast" />
        </div>
      </Transition>
    </section>
  </main>
  <div id="toast" :class="{ show: toastMessage }">{{ toastMessage }}</div>
  <AppDialog />
</template>
