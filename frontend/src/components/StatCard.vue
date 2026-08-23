<script setup lang="ts">
defineProps<{ label: string; value: string | number; hint: string; tone?: string; eyebrow?: string }>()
</script>

<template>
  <div class="card stat" :class="tone ? `stat-${tone}` : ''">
    <small v-if="eyebrow" class="stat-eyebrow">{{ eyebrow }}</small>
    <label>{{ label }}</label>
    <strong>{{ value }}</strong>
    <small class="stat-hint" :class="tone">{{ hint }}</small>
  </div>
</template>
