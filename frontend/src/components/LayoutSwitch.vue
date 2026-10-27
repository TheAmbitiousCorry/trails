<script setup lang="ts">
import { LAYOUTS } from '../layoutChoice'
import type { LayoutKind } from '../mapLayout'

defineProps<{ layout: LayoutKind }>()
defineEmits<{ choose: [l: LayoutKind] }>()
</script>

<template>
  <div class="layout-switch" role="group" aria-label="Layout">
    <button v-for="l in LAYOUTS" :key="l.id" type="button" :aria-pressed="layout === l.id" @click="$emit('choose', l.id)">{{ l.label }}</button>
  </div>
</template>

<style scoped>
.layout-switch { display: flex; gap: 0.35rem; }
.layout-switch button { font: inherit; padding: 0.35rem 0.7rem; border: 0; border-radius: 6px; background: var(--s2); color: var(--fg); cursor: pointer; }
.layout-switch button[aria-pressed='true'] { background: var(--accent); color: var(--bg); font-weight: 500; }
</style>
