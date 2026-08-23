<script setup lang="ts">
defineProps<{ label: string; value: string | number; hint: string; tone?: string }>()
</script>

<template>
  <div class="card stat">
    <label>{{ label }}</label>
    <strong>{{ value }}</strong>
    <small :class="tone">{{ hint }}</small>
  </div>
</template>
