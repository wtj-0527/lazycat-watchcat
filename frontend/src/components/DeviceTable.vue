<script setup lang="ts">
import type { Device } from '@/types'
import { ago, metricValue } from '@/utils'
import StatusPill from './StatusPill.vue'

defineProps<{ items: Device[]; clickable?: boolean }>()
const emit = defineEmits<{ select: [id: string] }>()
</script>

<template>
  <table>
    <thead>
      <tr><th>设备</th><th>CPU/负载</th><th>内存</th><th>存储</th><th>最后上报</th><th>状态</th></tr>
    </thead>
    <tbody>
      <tr
        v-for="device in items"
        :key="device.id"
        :class="{ 'device-row': clickable }"
        @click="clickable && emit('select', device.id)"
      >
        <td class="device">
          <b>{{ device.name }}</b>
          <small>{{ device.hostname }} · {{ device.osVersion || '系统版本未知' }}</small>
        </td>
        <td>{{ metricValue(device, 'system.cpu.usage') !== '—' ? metricValue(device, 'system.cpu.usage') : metricValue(device, 'system.load.1m', 2) }}</td>
        <td>{{ metricValue(device, 'system.memory.usage') }}</td>
        <td>{{ metricValue(device, 'filesystem.root.usage') }}</td>
        <td>{{ ago(device.lastSeenAt) }}<template v-if="device.stale"> · 数据过期</template></td>
        <td><StatusPill :status="device.status === 'revoked' ? 'revoked' : device.online ? device.health : 'offline'" /></td>
      </tr>
    </tbody>
  </table>
</template>
