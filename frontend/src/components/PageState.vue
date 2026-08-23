<script setup lang="ts">
defineProps<{ loading: boolean; error: string; empty?: boolean; emptyTitle?: string; emptyText?: string }>()
</script>

<template>
  <div v-if="loading" class="card loading">正在读取实时数据…</div>
  <div v-else-if="error" class="card empty"><h2>数据加载失败</h2><p class="muted">{{ error }}</p></div>
  <div v-else-if="empty" class="card empty"><h2>{{ emptyTitle }}</h2><p class="muted">{{ emptyText }}</p></div>
  <slot v-else />
</template>
