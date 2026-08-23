<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '@/api'
import { usePolling } from '@/composables'
import type { Inspection } from '@/types'
import { ago, dateTime, inspectionState, signed } from '@/utils'
import PageState from '@/components/PageState.vue'
import StatCard from '@/components/StatCard.vue'
import StatusPill from '@/components/StatusPill.vue'

interface Payload { items: Inspection[]; detail?: Inspection }
const emit = defineEmits<{ toast: [message: string] }>()
const running = ref(false)
const selected = ref<Inspection>()
const detailLoading = ref(false)
const { data, loading, error, refresh } = usePolling(async (): Promise<Payload> => {
  const list = await api<{ items: Inspection[] | null }>('/api/v1/inspections')
  const items = list.items || []
  const detail = items[0] ? await api<Inspection>(`/api/v1/inspections/${encodeURIComponent(items[0].id)}`) : undefined
  return { items, detail }
})
const latest = computed(() => selected.value || data.value?.detail || data.value?.items[0])
const reportChecks = computed(() => latest.value?.report?.checks || {})
const connectivityCheckState = computed(() => {
  const online = reportChecks.value.online
  const devices = reportChecks.value.devices
  if (typeof online !== 'number' || typeof devices !== 'number' || devices === 0) return 'unknown'
  return online === devices ? 'healthy' : 'warning'
})

async function start() {
  running.value = true
  try {
    const created = await api<Inspection>('/api/v1/inspections', { method: 'POST' })
    const readback = await api<Inspection>(`/api/v1/inspections/${encodeURIComponent(created.id)}`)
    selected.value = readback
    emit('toast', '正式巡检已完成、保存并回读报告')
    const refreshed = await refresh()
    if (refreshed?.detail?.id === readback.id) selected.value = undefined
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    running.value = false
  }
}
async function selectReport(item: Inspection) {
  detailLoading.value = true
  try {
    selected.value = await api<Inspection>(`/api/v1/inspections/${encodeURIComponent(item.id)}`)
  } catch (reason) {
    emit('toast', reason instanceof Error ? reason.message : String(reason))
  } finally {
    detailLoading.value = false
  }
}
</script>

<template>
  <PageState :loading="loading" :error="error" @retry="refresh">
    <div class="page-intro">
      <div><h2>巡检报告</h2><p>Fleet 全量巡检 · 固定快照与原始证据。</p></div>
      <button class="primary-button" :disabled="running" @click="start">{{ running ? '巡检中…' : '立即巡检' }}</button>
    </div>

    <template v-if="latest">
      <section class="inspection-hero card">
        <div>
          <div class="inspection-title"><h2>巡检报告 #{{ latest.id.slice(0, 8) }}</h2><StatusPill :status="inspectionState(latest)" /></div>
          <p>执行时间 {{ dateTime(latest.startedAt) }} · {{ latest.triggerType === 'manual' ? '手动巡检' : latest.triggerType }}</p>
          <small>证据 SHA-256：<code>{{ latest.evidenceSha256 }}</code></small>
        </div>
        <div class="button-row"><button class="secondary-button" disabled>导出 JSON</button><button class="secondary-button" disabled>导出 PDF 报告</button></div>
      </section>

      <div class="stats four">
        <StatCard label="巡检设备" :value="latest.deviceCount" hint="全部设备" />
        <StatCard label="通过" :value="latest.healthyCount" :hint="latest.deviceCount ? `${(latest.healthyCount / latest.deviceCount * 100).toFixed(1)}%` : 'Unknown'" tone="green" />
        <StatCard label="Warning" :value="latest.warningCount" :hint="`较上次 ${signed(latest.changeSummary?.warningDelta)}`" tone="amber" />
        <StatCard label="Critical" :value="latest.criticalCount" :hint="`较上次 ${signed(latest.changeSummary?.criticalDelta)}`" tone="red" />
      </div>

      <div class="inspection-layout">
        <section class="card inspection-results">
          <div class="section-title"><div><h2>分类检查结果</h2><span class="muted">来自已保存报告，不使用模拟检查项</span></div></div>
          <div class="check-row"><div><b>设备连接</b><span>在线设备与设备总数</span></div><strong>{{ reportChecks.online ?? 'Unknown' }} / {{ reportChecks.devices ?? latest.deviceCount }}</strong><StatusPill :status="connectivityCheckState" /></div>
          <div class="check-row"><div><b>健康规则</b><span>Collector 阈值判断</span></div><strong>{{ reportChecks.healthy ?? latest.healthyCount }} 通过</strong><StatusPill :status="inspectionState(latest)" /></div>
          <div class="check-row"><div><b>存储与 SMART</b><span>报告内设备原始指标可追溯</span></div><strong>{{ latest.report?.devices ? '已包含快照' : 'Unknown' }}</strong><StatusPill :status="latest.report?.devices ? 'available' : 'unknown'" /></div>
          <div class="check-row"><div><b>应用与容器</b><span>独立分类结果</span></div><strong>Contract gap</strong><StatusPill status="unknown" /></div>
          <div class="check-row"><div><b>通知投递</b><span>巡检报告未包含通知回执</span></div><strong>Contract gap</strong><StatusPill status="unknown" /></div>
        </section>
        <aside class="card evidence-panel">
          <div class="section-title compact"><div><h2>证据摘要</h2><span class="muted">Immutable report evidence</span></div></div>
          <dl class="definition-list">
            <div><dt>Schema</dt><dd>v{{ latest.report?.schemaVersion ?? 'Unknown' }}</dd></div>
            <div><dt>Source</dt><dd>{{ latest.report?.source || 'Unknown' }}</dd></div>
            <div><dt>生成时间</dt><dd>{{ dateTime(latest.report?.generatedAt) }}</dd></div>
            <div><dt>最近指标</dt><dd>{{ ago(latest.report?.latestMetricAt) }}</dd></div>
            <div><dt>新增告警</dt><dd>{{ latest.changeSummary?.newAlerts?.length || 0 }}</dd></div>
            <div><dt>恢复告警</dt><dd>{{ latest.changeSummary?.resolvedAlerts?.length || 0 }}</dd></div>
          </dl>
          <div class="hash-box"><span>SHA-256</span><code>{{ latest.evidenceSha256 }}</code></div>
        </aside>
      </div>
    </template>

    <section class="card report-history">
      <div class="section-title"><div><h2>巡检历史</h2><span class="muted">每日 03:00、每周日 04:00 自动运行；失败最多重试 3 次</span></div></div>
      <div v-if="data?.items.length" class="table-scroll">
        <table class="fleet-table"><thead><tr><th>报告</th><th>时间 / 类型</th><th>设备</th><th>健康</th><th>Warning</th><th>Critical</th><th>证据</th></tr></thead>
          <tbody><tr v-for="item in data.items" :key="item.id" class="device-row" @click="selectReport(item)">
            <td><button class="row-link" @click.stop="selectReport(item)">#{{ item.id.slice(0, 8) }}</button><small>{{ item.status }}</small></td>
            <td>{{ dateTime(item.startedAt) }}<small>{{ item.triggerType }}</small></td>
            <td>{{ item.deviceCount }}</td><td class="green">{{ item.healthyCount }}</td><td class="amber">{{ item.warningCount }}</td><td class="red">{{ item.criticalCount }}</td>
            <td><code>{{ item.evidenceSha256.slice(0, 16) }}…</code></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="inline-empty">尚无巡检记录。可立即执行第一次正式巡检。</div>
      <div v-if="detailLoading" class="detail-loading">正在读取报告证据…</div>
    </section>
  </PageState>
</template>
