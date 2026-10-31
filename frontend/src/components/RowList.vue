<script setup lang="ts">
defineProps<{ label: string; addLabel: string; placeholder?: string }>()
const rows = defineModel<string[]>({ required: true })
</script>

<template>
  <fieldset class="rowlist">
    <legend>{{ label }}</legend>
    <p v-if="!rows.length" class="muted small">None.</p>
    <div v-for="(_, i) in rows" :key="i" class="rowlist-row">
      <input v-model="rows[i]" type="text" :aria-label="`${label} ${i + 1}`" :placeholder="placeholder" autocomplete="off" spellcheck="false" />
      <button type="button" class="btn btn-quiet" :aria-label="`Remove ${label} ${i + 1}`" @click="rows.splice(i, 1)">Remove</button>
    </div>
    <button type="button" class="btn btn-quiet" @click="rows.push('')">{{ addLabel }}</button>
  </fieldset>
</template>
