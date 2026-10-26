<script setup lang="ts">
import { useApi, when, type SessionRow } from '../api'
import Load from '../components/Load.vue'

const { data, error, loading } = useApi<{ sessions: SessionRow[] }>(() => '/api/sessions')
</script>

<template>
  <h1>Sessions</h1>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.sessions.length" class="muted">No session has started yet.</p>
    <ul v-if="data" class="cards">
      <li v-for="s in data.sessions" :key="s.id" class="card">
        <h2 class="card-title"><RouterLink :to="{ name: 'session', params: { id: s.id } }">{{ s.task }}</RouterLink></h2>
        <p class="muted small">
          {{ s.actor }}, {{ when(s.started_at) }},
          <template v-if="s.ended_by">ended by {{ s.ended_by }}</template><template v-else>still open</template>
        </p>
        <p class="badges">
          <span class="count">{{ s.counts.looks }} looks</span>
          <span class="count">{{ s.counts.searches }} searches</span>
          <span class="count">{{ s.counts.reads }} reads</span>
          <span v-if="s.counts.writes" class="count">{{ s.counts.writes }} writes</span>
          <span v-if="s.counts.confirms" class="count">{{ s.counts.confirms }} confirms</span>
          <span v-if="s.counts.off_trail" class="pill pill-kind">{{ s.counts.off_trail }} off trail</span>
          <span v-if="s.counts.misses" class="pill pill-gap">{{ s.counts.misses }} {{ s.counts.misses === 1 ? 'miss' : 'misses' }}</span>
        </p>
      </li>
    </ul>
  </Load>
</template>
