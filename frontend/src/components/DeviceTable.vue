<script setup lang="ts">
import type { Device } from '@/types'
import { ago, deviceState, metricValueAny } from '@/utils'
import StatusPill from './StatusPill.vue'

defineProps<{ items: Device[]; clickable?: boolean }>()
const emit = defineEmits<{ select: [id: string] }>()
</script>

<template>
  <div class="table-scroll">
  <table class="fleet-table">
    <thead>
      <tr><th>设备</th><th>设备组</th><th>状态</th><th>系统</th><th>CPU</th><th>内存</th><th>存储</th><th>应用</th><th>最后上报</th></tr>
    </thead>
    <tbody>
      <tr
        v-for="device in items"
        :key="device.id"
        :class="{ 'device-row': clickable }"
        @click="clickable && emit('select', device.id)"
        @keydown.enter="clickable && emit('select', device.id)"
        @keydown.space.prevent="clickable && emit('select', device.id)"
        :tabindex="clickable ? 0 : undefined"
      >
        <td class="device">
          <b>{{ device.name }}</b>
          <small>{{ device.hostname || '主机名未知' }}</small>
        </td>
        <td><span class="contract-gap">未分组</span></td>
        <td><StatusPill :status="deviceState(device)" /></td>
        <td>{{ device.osVersion || 'Unknown' }}<small>Collector {{ device.collectorVersion || 'Unknown' }}</small></td>
        <td>{{ metricValueAny(device, ['system.cpu.usage', 'system.load.1m']) }}</td>
        <td>{{ metricValueAny(device, ['system.memory.usage']) }}</td>
        <td>{{ metricValueAny(device, ['filesystem.root.usage', 'btrfs.usage']) }}</td>
        <td><span class="contract-gap">Contract gap</span></td>
        <td>{{ ago(device.lastSeenAt) }}<small v-if="device.stale">数据已过期</small></td>
      </tr>
    </tbody>
  </table>
  </div>
</template>
