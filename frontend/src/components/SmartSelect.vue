<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AppIcon from './AppIcon.vue'

export interface SmartOption { value: string; label: string; meta?: string; group?: string; status?: string }
const props = defineProps<{ modelValue: string; options: SmartOption[]; allLabel: string; controlLabel: string; searchable?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const root = ref<HTMLElement>()
const open = ref(false)
const query = ref('')
const selected = computed(() => props.options.find((item) => item.value === props.modelValue))
const filtered = computed(() => {
  const value = query.value.trim().toLowerCase()
  return value ? props.options.filter((item) => `${item.label} ${item.meta || ''} ${item.group || ''}`.toLowerCase().includes(value)) : props.options
})
function choose(value: string) { emit('update:modelValue', value); open.value = false; query.value = '' }
function outside(event: MouseEvent) { if (!root.value?.contains(event.target as Node)) open.value = false }
onMounted(() => document.addEventListener('mousedown', outside))
onBeforeUnmount(() => document.removeEventListener('mousedown', outside))
</script>

<template>
  <div ref="root" class="smart-select" :class="{ open }">
    <button type="button" class="smart-select-trigger" :aria-label="controlLabel" :aria-expanded="open" @click="open = !open">
      <span><b>{{ selected?.label || allLabel }}</b><small v-if="selected?.meta">{{ selected.meta }}</small></span>
      <AppIcon name="chevron-down" :size="16" />
    </button>
    <div v-if="open" class="smart-select-menu">
      <label v-if="searchable" class="smart-select-search"><AppIcon name="search" :size="15" /><input v-model="query" autofocus placeholder="搜索用户、设备或部署 ID"></label>
      <div class="smart-select-options">
        <button type="button" :class="{ selected: modelValue === 'all' }" @click="choose('all')"><span><b>{{ allLabel }}</b></span><i v-if="modelValue === 'all'">✓</i></button>
        <button v-for="item in filtered" :key="item.value" type="button" :class="{ selected: modelValue === item.value }" @click="choose(item.value)">
          <span class="smart-option-copy"><small v-if="item.group">{{ item.group }}</small><b>{{ item.label }}</b><em v-if="item.meta">{{ item.meta }}</em></span>
          <span v-if="item.status" class="smart-option-status" :class="item.status">{{ item.status === 'running' ? '运行' : item.status === 'error' ? '异常' : '暂停' }}</span>
          <i v-if="modelValue === item.value">✓</i>
        </button>
        <div v-if="!filtered.length" class="smart-select-empty">没有匹配项</div>
      </div>
    </div>
  </div>
</template>
