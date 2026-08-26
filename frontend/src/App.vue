<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '@/api'
import type { Overview } from '@/types'
import { ago } from '@/utils'
import OverviewPage from '@/pages/OverviewPage.vue'
import DevicesPage from '@/pages/DevicesPage.vue'
import AppsPage from '@/pages/AppsPage.vue'
import StoragePage from '@/pages/StoragePage.vue'
import AlertsPage from '@/pages/AlertsPage.vue'
import InspectionsPage from '@/pages/InspectionsPage.vue'
import SettingsPage from '@/pages/SettingsPage.vue'
import AppIcon from '@/components/AppIcon.vue'

type Page = 'overview' | 'devices' | 'apps' | 'storage' | 'alerts' | 'inspections' | 'onboarding' | 'settings'
const navs: Array<[Page, string]> = [
  ['overview', '总览'], ['devices', '设备'], ['apps', '应用'],
  ['storage', '存储'], ['alerts', '告警'], ['inspections', '巡检'],
  ['onboarding', '接入'], ['settings', '设置'],
]
const pages = {
  overview: OverviewPage,
  devices: DevicesPage,
  apps: AppsPage,
  storage: StoragePage,
  alerts: AlertsPage,
  inspections: InspectionsPage,
  onboarding: SettingsPage,
  settings: SettingsPage,
}

const page = ref<Page>('overview')
const version = ref('—')
const fleet = ref<Overview>()
const toastMessage = ref('')
const globalQuery = ref('')
const searchNonce = ref(0)
let toastTimer: number | undefined
let shellTimer: number | undefined
let shellLoading = false

const pageComponent = computed(() => pages[page.value])
const pageLabel = computed(() => navs.find(([key]) => key === page.value)?.[1] || '总览')
const pageProps = computed(() => page.value === 'settings'
  ? { initialTab: 'thresholds' }
  : page.value === 'onboarding' ? { initialTab: 'onboarding' } : {})
const deviceCount = computed(() => fleet.value?.stats.devices ?? 0)
const onlineCount = computed(() => fleet.value?.stats.online ?? 0)
const staleCount = computed(() => fleet.value?.devices.filter((device) => device.stale).length ?? 0)
const freshness = computed(() => ago(fleet.value?.updatedAt))

function pageFromHash(): Page {
  const candidate = location.hash.slice(1) as Page
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
async function loadShell() {
  if (shellLoading) return
  shellLoading = true
  try {
    const result = await api<Overview>('/api/v1/overview')
    fleet.value = { ...result, devices: result.devices || [], alerts: result.alerts || [] }
  } catch {
    // Page-level error states remain authoritative when shell telemetry is unavailable.
  } finally {
    shellLoading = false
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

onMounted(async () => {
  syncHash()
  window.addEventListener('hashchange', syncHash)
  void loadShell()
  shellTimer = window.setInterval(loadShell, 30_000)
  try {
    version.value = (await api<{ version: string }>('/api/v1/version')).version
  } catch {
    version.value = '—'
  }
})
onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncHash)
  window.clearTimeout(toastTimer)
  window.clearInterval(shellTimer)
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
      </div>
      <div class="operator-card">
        <span class="operator-avatar">王</span>
        <div><b>设备管理员</b><small>可处置告警 · v{{ version }}</small></div>
      </div>
    </div>
  </aside>
  <main class="app-main">
    <header class="topbar">
      <div class="topbar-context">
        <span class="topbar-page-name">{{ pageLabel }}</span>
        <div class="scope-picker" title="当前为全局设备范围">
          <span>全部设备</span><b>{{ deviceCount }} 台</b>
        </div>
      </div>
      <div class="topbar-actions">
        <span class="freshness-pill" :class="{ stale: staleCount }">更新 {{ freshness }}</span>
        <form class="global-search" role="search" @submit.prevent="submitGlobalSearch">
          <AppIcon name="search" :size="16" />
          <input v-model="globalQuery" aria-label="全局搜索" placeholder="搜索设备、应用、告警...">
        </form>
        <button class="icon-button" type="button" aria-label="通知" @click="navigate('alerts')"><AppIcon name="alerts" :size="17" /></button>
        <button class="user-avatar" type="button" aria-label="当前用户设置" @click="navigate('settings')">王</button>
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
</template>
