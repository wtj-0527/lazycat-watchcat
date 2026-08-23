<script setup lang="ts">
defineProps<{ loading: boolean; error: string; empty?: boolean; emptyTitle?: string; emptyText?: string }>()
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div v-if="loading" class="card page-state">
    <span class="spinner" />
    <h2>正在读取实时数据</h2>
    <p class="muted">Collector 与服务状态返回后将自动更新。</p>
  </div>
  <div v-else-if="error" class="card page-state error-state">
    <span class="state-symbol">!</span>
    <h2>数据加载失败</h2>
    <p class="muted">{{ error }}</p>
    <button class="secondary-button" @click="emit('retry')">重新加载</button>
  </div>
  <div v-else-if="empty" class="card page-state">
    <span class="state-symbol neutral">—</span>
    <h2>{{ emptyTitle }}</h2>
    <p class="muted">{{ emptyText }}</p>
  </div>
  <slot v-else />
</template>
