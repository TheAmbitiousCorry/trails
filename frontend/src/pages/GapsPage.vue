<script setup lang="ts">
import { useApi, when, type Gap } from '../api'
import Load from '../components/Load.vue'

const { data, error, loading } = useApi<{ gaps: Gap[] }>(() => '/api/gaps?state=open')
</script>

<template>
  <h1>Gap queue</h1>
  <p class="muted">Searches that found nothing and paths read off the trail, still waiting for an answer.</p>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.gaps.length" class="muted">No open gaps.</p>
    <ul v-if="data" class="cards">
      <li v-for="g in data.gaps" :key="g.id" class="card">
        <h2 class="card-title">{{ g.query || g.paths }}</h2>
        <p class="small">
          At
          <RouterLink v-if="g.sign_id" :to="{ name: 'sign', params: { id: g.sign_id } }">{{ g.sign_title }}</RouterLink><span v-else class="muted">no sign</span>,
          from <RouterLink :to="{ name: 'session', params: { id: g.session_id } }">{{ g.task || g.session_id }}</RouterLink>
        </p>
        <p class="muted small">gap {{ g.id }}, opened {{ when(g.opened_at) }}</p>
      </li>
    </ul>
  </Load>
</template>
