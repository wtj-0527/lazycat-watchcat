<script setup lang="ts">
import type { Device } from '@/types'
import { ago, deviceState, metricValueAny } from '@/utils'
import StatusPill from './StatusPill.vue'

defineProps<{ items: Device[]; clickable?: boolean }>()
defineEmits<{ select: [id: string] }>()

function capabilitySummary(device: Device): string {
  const names = Object.keys(device.latest || {})
  if (!names.length) return '未知'
  const available = ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.'].filter((prefix) => names.some((name) => name.startsWith(prefix))).length
  return available >= 4 ? '全部可用' : `可用 ${available} · 受限 ${Math.max(0, 5 - available)}`
}
</script>

<template>
  <div class="table-scroll">
  <table class="fleet-table device-inventory-table">
    <thead><tr><th aria-label="选择" /><th>设备</th><th>健康</th><th>连接</th><th>采集能力</th><th>资源摘要</th><th>最新数据</th><th>告警</th><th /></tr></thead>
    <tbody>
      <tr v-for="device in items" :key="device.id" :class="{ 'device-row': clickable }" @click="clickable && $emit('select', device.id)">
        <td><input type="checkbox" aria-label="选择设备" @click.stop></td>
        <td class="device device-with-mark">
          <span class="device-mark" :class="deviceState(device)" />
          <span><button v-if="clickable" class="row-link" @click.stop="$emit('select', device.id)">{{ device.name }}</button><b v-else>{{ device.name }}</b><small>{{ device.location || device.hostname || '位置未设置' }} · {{ device.group || '未分组' }}</small></span>
        </td>
        <td><StatusPill :status="deviceState(device)" /></td>
        <td><b :class="device.online ? 'green' : 'red'">{{ device.online ? (device.stale ? '陈旧' : '在线') : '离线' }}</b></td>
        <td>{{ capabilitySummary(device) }}</td>
        <td>处理器 {{ metricValueAny(device, ['system.cpu.usage', 'system.load.1m']) }} · 内存 {{ metricValueAny(device, ['system.memory.usage']) }}</td>
        <td>{{ ago(device.lastSeenAt) }}<small v-if="device.stale">数据已过期</small></td>
        <td><span class="alert-count">{{ deviceState(device) === 'critical' ? 2 : deviceState(device) === 'warning' ? 1 : 0 }}</span></td>
        <td><button v-if="clickable" class="row-link row-view" @click.stop="$emit('select', device.id)">查看 →</button></td>
      </tr>
    </tbody>
  </table>
  </div>
</template>
