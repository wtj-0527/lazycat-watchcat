<script setup lang="ts">
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Overview } from '@/types'
import { ago } from '@/utils'
import AlertRow from '@/components/AlertRow.vue'
import DeviceTable from '@/components/DeviceTable.vue'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'

const { data, loading, error } = usePolling(() => api<Overview>('/api/v1/overview'))
</script>

<template>
  <PageState :loading="loading" :error="error">
    <div class="stats">
      <StatCard label="设备" :value="data?.stats.devices ?? 0" hint="已注册" />
      <StatCard label="在线" :value="data?.stats.online ?? 0" hint="90 秒内上报" tone="green" />
      <StatCard label="离线" :value="data?.stats.offline ?? 0" hint="需要检查" tone="amber" />
      <StatCard label="Critical" :value="data?.stats.critical ?? 0" hint="实时阈值" tone="red" />
      <StatCard label="Warning" :value="data?.stats.warning ?? 0" hint="实时阈值" tone="amber" />
      <StatCard label="健康" :value="data?.stats.healthy ?? 0" :hint="`更新 ${ago(data?.updatedAt)}`" tone="green" />
    </div>
    <div class="grid">
      <div class="card">
        <div class="section-title"><div><h2>设备健康矩阵</h2><span class="muted">来自 Collector 的最新真实指标</span></div></div>
        <DeviceTable v-if="data?.devices.length" :items="data.devices" />
        <p v-else class="muted">尚未接入设备。</p>
      </div>
      <div>
        <div class="card">
          <h2>当前风险</h2>
          <p class="muted">{{ data?.alerts.length ?? 0 }} 条活动风险</p>
          <AlertRow v-for="alert in data?.alerts.slice(0, 8)" :key="alert.fingerprint" :alert="alert" />
          <p v-if="!data?.alerts.length" class="green">当前没有达到阈值的风险。</p>
        </div>
        <div class="card card-spaced">
          <h2>数据新鲜度</h2>
          <p class="muted">最近一次采集：{{ ago(data?.updatedAt) }}</p>
          <p>页面每 30 秒自动刷新。过期和离线设备不会被显示为健康。</p>
        </div>
      </div>
    </div>
  </PageState>
</template>
