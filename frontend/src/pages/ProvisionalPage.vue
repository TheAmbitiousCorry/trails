<script setup lang="ts">
import { useApi, type ProvisionalRow, projectApi } from '../api'
import Load from '../components/Load.vue'

const { data, error, loading } = useApi<{ entries: ProvisionalRow[] }>(() => projectApi('/provisional'))
</script>

<template>
  <h1>Provisional queue</h1>
  <p class="muted">Entries written by agents and not yet verified, most read first.</p>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.entries.length" class="muted">Nothing is waiting for verification.</p>
    <ul v-if="data" class="cards">
      <li v-for="e in data.entries" :key="e.id" class="card">
        <h2 class="card-title"><RouterLink :to="{ name: 'entry', params: { id: e.id } }">{{ e.title }}</RouterLink></h2>
        <p class="read-when">{{ e.read_when }}</p>
        <p class="badges">
          <span class="chip chip-kind">{{ e.kind }}</span>
          <span class="count">{{ e.reads }} {{ e.reads === 1 ? 'read' : 'reads' }} ({{ e.reads_30d }} in 30 days)</span>
          <span class="count">{{ e.confirmed_by }} confirmed</span>
          <span class="muted small">{{ e.path.map((p) => p.title).join(' / ') }}</span>
        </p>
      </li>
    </ul>
  </Load>
</template>
