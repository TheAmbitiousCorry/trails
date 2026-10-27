<script setup lang="ts">
import { useApi, type ProjectRow } from '../api'
import Load from '../components/Load.vue'

const { data, error, loading } = useApi<{ projects: ProjectRow[] }>(() => '/api/projects')
</script>

<template>
  <h1>Projects</h1>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.projects.length" class="muted">This server holds no project. Run <code>trails register</code> in a repo to add one.</p>
    <ul v-if="data && data.projects.length" class="projects">
      <li v-for="p in data.projects" :key="p.id">
        <RouterLink class="project-title" :to="{ name: 'map', params: { project: p.id } }">{{ p.title }}</RouterLink>
        <span class="project-counts muted small">
          <span>{{ p.entries }} {{ p.entries === 1 ? 'entry' : 'entries' }}</span>
          <span>{{ p.open_sessions }} open {{ p.open_sessions === 1 ? 'session' : 'sessions' }}</span>
          <span>{{ p.open_gaps }} open {{ p.open_gaps === 1 ? 'gap' : 'gaps' }}</span>
        </span>
      </li>
    </ul>
  </Load>
</template>
