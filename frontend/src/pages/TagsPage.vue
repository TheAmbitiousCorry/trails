<script setup lang="ts">
import { useApi, type TagRow } from '../api'
import Load from '../components/Load.vue'

const { data, error, loading } = useApi<{ tags: TagRow[] }>(() => '/api/tags')
</script>

<template>
  <h1>Tags</h1>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.tags.length" class="muted">No entry carries a tag yet.</p>
    <ul v-if="data" class="cards">
      <li v-for="t in data.tags" :key="t.id" class="card" :class="{ undefined: !t.defined }">
        <h2 class="card-title">
          <RouterLink :to="{ name: 'map', query: { tag: t.id } }">{{ t.id }}</RouterLink>
          <span v-if="!t.defined" class="chip chip-gap">undefined</span>
        </h2>
        <p v-if="t.description">{{ t.description }}</p>
        <p v-else class="muted small">No description.</p>
        <p class="muted small">{{ t.uses }} {{ t.uses === 1 ? 'entry' : 'entries' }}</p>
      </li>
    </ul>
  </Load>
</template>
