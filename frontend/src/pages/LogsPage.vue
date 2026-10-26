<script setup lang="ts">
import { useApi, when, type Gap, type Rejections } from '../api'
import Load from '../components/Load.vue'

const rej = useApi<Rejections>(() => '/api/rejections')
const dis = useApi<{ dismissals: Gap[] }>(() => '/api/dismissals')
</script>

<template>
  <h1>Rejections and dismissals</h1>

  <h2>Rejections</h2>
  <p class="muted">Calls the server refused, by rule and who made them. One rule dominating means its cap is working or mistuned.</p>
  <Load :ready="!!rej.data.value" :loading="rej.loading.value" :error="rej.error.value">
    <template v-if="rej.data.value">
      <p v-if="!rej.data.value.groups.length" class="muted">No call has been refused.</p>
      <div v-else class="table-wrap">
        <table>
          <thead><tr><th scope="col">Rule</th><th scope="col">Actor</th><th scope="col" class="num">Count</th></tr></thead>
          <tbody>
            <tr v-for="g in rej.data.value.groups" :key="g.rule + g.actor_kind"><td><code>{{ g.rule }}</code></td><td>{{ g.actor_kind }}</td><td class="num">{{ g.count }}</td></tr>
          </tbody>
        </table>
      </div>
      <h3>Most recent</h3>
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col">When</th><th scope="col">Tool</th><th scope="col">Rule</th><th scope="col">Target</th></tr></thead>
          <tbody>
            <tr v-for="(r, i) in rej.data.value.recent" :key="i">
              <td>{{ when(r.at) }}</td><td>{{ r.tool }}</td><td><code>{{ r.rule }}</code></td>
              <td><RouterLink :to="{ name: 'session', params: { id: r.session_id } }">{{ r.target || 'session' }}</RouterLink></td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </Load>

  <h2>Dismissals</h2>
  <p class="muted">Gaps an agent closed as not worth filling, with the reason given.</p>
  <Load :ready="!!dis.data.value" :loading="dis.loading.value" :error="dis.error.value">
    <template v-if="dis.data.value">
      <p v-if="!dis.data.value.dismissals.length" class="muted">No gap has been dismissed.</p>
      <div v-else class="table-wrap">
        <table>
          <thead><tr><th scope="col">Query</th><th scope="col">Reason</th><th scope="col">Session</th><th scope="col">When</th></tr></thead>
          <tbody>
            <tr v-for="g in dis.data.value.dismissals" :key="g.id">
              <td>{{ g.query || g.paths }}</td><td>{{ g.reason }}</td>
              <td><RouterLink :to="{ name: 'session', params: { id: g.session_id } }">{{ g.task || g.session_id }}</RouterLink></td>
              <td>{{ when(g.resolved_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </Load>
</template>
