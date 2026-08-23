<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '@/api'
import OverviewPage from '@/pages/OverviewPage.vue'
import DevicesPage from '@/pages/DevicesPage.vue'
import AppsPage from '@/pages/AppsPage.vue'
import StoragePage from '@/pages/StoragePage.vue'
import AlertsPage from '@/pages/AlertsPage.vue'
import InspectionsPage from '@/pages/InspectionsPage.vue'
import SettingsPage from '@/pages/SettingsPage.vue'

type Page = 'overview' | 'devices' | 'apps' | 'storage' | 'alerts' | 'inspections' | 'settings'
const navs: Array<[Page, string, string]> = [
  ['overview', '⌂', '总览'], ['devices', '▦', '设备'], ['apps', '◇', '应用'],
  ['storage', '▤', '存储健康'], ['alerts', '!', '告警'], ['inspections', '✓', '巡检'], ['settings', '⚙', '设置'],
]
const titles: Record<Page, [string, string]> = {
  overview: ['Fleet Overview', '全部设备 · 实时健康与风险'],
  devices: ['设备', '实时设备状态与指标'],
  apps: ['应用', '跨设备 LPK 健康与版本'],
  storage: ['存储健康', '文件系统、Btrfs 与物理磁盘风险'],
  alerts: ['告警', '持久化状态、确认、静默与恢复'],
  inspections: ['巡检报告', '正式巡检记录与证据摘要'],
  settings: ['设置', '设备接入、数据保留与通知'],
}
const pages = { overview: OverviewPage, devices: DevicesPage, apps: AppsPage, storage: StoragePage, alerts: AlertsPage, inspections: InspectionsPage, settings: SettingsPage }

const page = ref<Page>('overview')
const version = ref('—')
const toastMessage = ref('')
let toastTimer: number | undefined

const pageComponent = computed(() => pages[page.value])
const heading = computed(() => titles[page.value])

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

onMounted(async () => {
  syncHash()
  window.addEventListener('hashchange', syncHash)
  try {
    version.value = (await api<{ version: string }>('/api/v1/version')).version
  } catch {
    version.value = '—'
  }
})
onBeforeUnmount(() => {
  window.removeEventListener('hashchange', syncHash)
  window.clearTimeout(toastTimer)
})
</script>

<template>
  <aside class="sidebar">
    <div class="brand"><div class="logo">🐱</div><div><b>猫眼</b><small>Fleet Monitoring</small><span id="app-version">v{{ version }}</span></div></div>
    <nav>
      <button v-for="[key, icon, label] in navs" :key="key" class="nav-item" :class="{ active: page === key }" @click="navigate(key)">
        <span>{{ icon }}</span>{{ label }}
      </button>
    </nav>
    <div class="hub"><i /><div><b>Monitor Hub</b><small>单 LPK · Vue 3</small></div></div>
  </aside>
  <main>
    <header>
      <div><h1>{{ heading[0] }}</h1><p>{{ heading[1] }}</p></div>
      <div class="actions"><button class="ghost">最近 24 小时⌄</button><button @click="navigate('inspections')">开始巡检</button></div>
    </header>
    <section id="content">
      <component :is="pageComponent" :key="page" @toast="toast" />
    </section>
  </main>
  <div id="toast" :class="{ show: toastMessage }">{{ toastMessage }}</div>
</template>
