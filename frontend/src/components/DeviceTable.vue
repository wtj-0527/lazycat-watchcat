<script setup lang="ts">
import type { Device } from '@/types'
import { ago, connectivityState, deviceState, metricValueAny } from '@/utils'
import AppIcon from './AppIcon.vue'
import StatusPill from './StatusPill.vue'

defineProps<{ items: Device[]; clickable?: boolean }>()
defineEmits<{ select: [id: string] }>()

function capabilitySummary(device: Device): string {
  const names = Object.keys(device.latest || {})
  if (!names.length) return '未知'
  const available = ['system.', 'container.', 'filesystem.', 'disk.', 'btrfs.'].filter((prefix) => names.some((name) => name.startsWith(prefix))).length
  return available >= 4 ? '全部可用' : `可用 ${available} · 受限 ${Math.max(0, 5 - available)}`
}

function connectivityLabel(device: Device): string {
  return ({ online: '在线', stale: '数据陈旧', offline: '离线' } as Record<string, string>)[connectivityState(device)]
}
</script>

<template>
  <div class="device-inventory-list">
    <article
      v-for="device in items"
      :key="device.id"
      class="device-inventory-item"
      :class="{ clickable }"
      @click="clickable && $emit('select', device.id)"
    >
      <div class="device-inventory-identity">
        <span class="device-glyph" :class="deviceState(device)"><AppIcon name="devices" :size="20" /></span>
        <div>
          <button v-if="clickable" class="row-link device-name" @click.stop="$emit('select', device.id)">{{ device.name }}</button>
          <b v-else class="device-name">{{ device.name }}</b>
          <small>{{ device.hostname || '主机名未知' }}<template v-if="device.location"> · {{ device.location }}</template><template v-if="device.group"> · {{ device.group }}</template></small>
        </div>
      </div>

      <div class="device-inventory-state">
        <StatusPill :status="deviceState(device)" />
        <span class="connectivity-label" :class="connectivityState(device)">
          <i />{{ connectivityLabel(device) }}
        </span>
      </div>

      <div class="device-resource-summary">
        <span><small>处理器</small><b>{{ metricValueAny(device, ['system.cpu.usage', 'system.load.1m']) }}</b></span>
        <span><small>内存</small><b>{{ metricValueAny(device, ['system.memory.usage']) }}</b></span>
      </div>

      <div class="device-inventory-meta">
        <span class="capability-chip">{{ capabilitySummary(device) }}</span>
        <span><small>最近更新</small><b>{{ ago(device.lastSeenAt) }}</b></span>
      </div>

      <button
        v-if="clickable"
        class="device-open-button"
        :aria-label="`查看设备 ${device.name}`"
        @click.stop="$emit('select', device.id)"
      >→</button>
    </article>
  </div>
</template>
