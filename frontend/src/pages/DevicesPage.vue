<script setup lang="ts">
import { ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Device, Metric, Overview } from '@/types'
import { ago, formatNumber, metricValue } from '@/utils'
import DeviceTable from '@/components/DeviceTable.vue'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

const selected = ref<Device>()
const detailLoading = ref(false)
const detailError = ref('')
const { data, loading, error } = usePolling(() => api<Overview>('/api/v1/overview'))

async function showDevice(id: string) {
  detailLoading.value = true
  detailError.value = ''
  try {
    selected.value = await api<Device>(`/api/v1/devices/${encodeURIComponent(id)}`)
  } catch (reason) {
    detailError.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    detailLoading.value = false
  }
}

function metrics(device: Device): Metric[] {
  return Object.values(device.latest || {}).flat()
}
</script>

<template>
  <div v-if="selected || detailLoading || detailError">
    <button class="ghost back" @click="selected = undefined; detailError = ''">← 返回设备清单</button>
    <PageState :loading="detailLoading" :error="detailError">
      <template v-if="selected">
        <div class="page-head">
          <div><h2>{{ selected.name }}</h2><span class="muted">{{ selected.hostname }} · 最后上报 {{ ago(selected.lastSeenAt) }}</span></div>
          <StatusPill :status="selected.online ? selected.health : 'offline'" />
        </div>
        <div class="stats four">
          <StatCard label="负载" :value="metricValue(selected, 'system.load.1m', 2)" hint="1 分钟" />
          <StatCard label="内存" :value="metricValue(selected, 'system.memory.usage')" hint="实时" />
          <StatCard label="根文件系统" :value="metricValue(selected, 'filesystem.root.usage')" hint="实时" />
          <StatCard label="Uptime" :value="metricValue(selected, 'system.uptime', 0)" hint="秒" />
        </div>
        <div class="card">
          <h2>最新指标</h2>
          <table v-if="metrics(selected).length">
            <thead><tr><th>指标</th><th>值</th><th>标签</th><th>采集时间</th></tr></thead>
            <tbody><tr v-for="point in metrics(selected)" :key="`${point.name}-${JSON.stringify(point.labels)}`">
              <td>{{ point.name }}</td><td>{{ formatNumber(point.value) }}{{ point.unit }}</td>
              <td>{{ Object.entries(point.labels || {}).map(([key, value]) => `${key}=${value}`).join(', ') }}</td><td>{{ ago(point.collectedAt) }}</td>
            </tr></tbody>
          </table>
          <p v-else class="muted">尚未收到指标。</p>
        </div>
      </template>
    </PageState>
  </div>
  <PageState v-else :loading="loading" :error="error">
    <div class="page-head"><div><h2>设备清单</h2><span class="muted">{{ data?.devices.length ?? 0 }} / 100 台已接入</span></div></div>
    <div class="card">
      <DeviceTable v-if="data?.devices.length" :items="data.devices" clickable @select="showDevice" />
      <p v-else class="muted">尚未接入设备。</p>
    </div>
  </PageState>
</template>
