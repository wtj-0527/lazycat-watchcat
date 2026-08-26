<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { dialogState, resolveDialog } from '@/dialog'

const input = ref<HTMLInputElement>()
const confirmButton = ref<HTMLButtonElement>()

watch(() => dialogState.open, async (open) => {
  if (!open) return
  await nextTick()
  if (dialogState.input) {
    input.value?.focus()
    input.value?.select()
  } else {
    confirmButton.value?.focus()
  }
})

function keydown(event: KeyboardEvent) {
  if (!dialogState.open) return
  if (event.key === 'Escape') resolveDialog(false)
}
window.addEventListener('keydown', keydown)
onBeforeUnmount(() => window.removeEventListener('keydown', keydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="dialog">
      <div v-if="dialogState.open" class="dialog-backdrop" role="presentation" @mousedown.self="resolveDialog(false)">
        <section class="app-dialog" role="dialog" aria-modal="true" :aria-labelledby="'app-dialog-title'">
          <div class="dialog-symbol" :class="{ danger: dialogState.danger }">{{ dialogState.danger ? '!' : '✓' }}</div>
          <div class="dialog-copy">
            <h2 id="app-dialog-title">{{ dialogState.title }}</h2>
            <p v-if="dialogState.message">{{ dialogState.message }}</p>
          </div>
          <form @submit.prevent="resolveDialog(true)">
            <input
              v-if="dialogState.input"
              ref="input"
              v-model="dialogState.value"
              class="dialog-input"
              :type="dialogState.inputType"
              :placeholder="dialogState.inputPlaceholder"
              autocomplete="off"
            >
            <div class="dialog-actions">
              <button type="button" class="secondary-button" @click="resolveDialog(false)">{{ dialogState.cancelText }}</button>
              <button ref="confirmButton" type="submit" :class="dialogState.danger ? 'danger-button solid' : 'primary-button'">{{ dialogState.confirmText }}</button>
            </div>
          </form>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
