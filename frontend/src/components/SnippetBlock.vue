<script setup lang="ts">
import { useApi, type Snippet } from '../api'

const props = defineProps<{ entryId: string; anchorId: number }>()
const { data, error } = useApi<Snippet>(() => `/api/entries/${encodeURIComponent(props.entryId)}/snippet?anchor=${props.anchorId}`)
</script>

<template>
  <figure v-if="data" class="snippet">
    <figcaption>
      <code>{{ data.path }}:{{ data.start }}-{{ data.end }}</code>
      <span v-if="!data.ok" class="pill pill-stale">anchor failed</span>
    </figcaption>
    <pre><code>{{ data.text }}</code></pre>
    <p v-if="data.truncated" class="muted small">Showing the first {{ data.end - data.start + 1 }} lines.</p>
  </figure>
  <p v-else-if="error" class="muted small">Snippet unavailable: {{ error }}</p>
</template>
