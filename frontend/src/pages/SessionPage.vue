<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useApi, when, type RouteStep, type SessionResponse } from '../api'
import Load from '../components/Load.vue'
import TierBadge from '../components/TierBadge.vue'

const route = useRoute()
const { data, error, loading } = useApi<SessionResponse>(() => `/api/sessions/${encodeURIComponent(String(route.params.id))}`)

const how = computed(() => {
  const m = new Map<string, string[]>()
  for (const t of data.value?.touched ?? []) m.set(t.id, [...(m.get(t.id) ?? []), t.how])
  return m
})

const verbs: Record<string, string> = {
  look: 'looked at', search: 'searched', read: 'read', write: 'wrote under', confirm: 'confirmed',
  resolve_gap: 'resolved gap', session_end: 'ended the session',
}

function verb(s: RouteStep): string {
  if (s.tool === 'read' && s.depth) return `read (${s.depth})`
  return verbs[s.tool] ?? s.tool
}
</script>

<template>
  <Load :ready="!!data" :loading="loading" :error="error">
    <template v-if="data">
      <p class="crumbs"><RouterLink to="/sessions">Sessions</RouterLink></p>
      <h1 class="task">{{ data.session.task }}</h1>
      <p class="muted">
        {{ data.session.actor }}, {{ data.session.task_type }}, started {{ when(data.session.started_at) }},
        <template v-if="data.session.ended_by">ended by {{ data.session.ended_by }}</template><template v-else>still open</template>
      </p>

      <div class="trail-layout">
        <section aria-labelledby="route-h">
          <h2 id="route-h">Route</h2>
          <p v-if="!data.route.length" class="muted">This session made no calls after it started.</p>
          <ol class="timeline">
            <li v-for="s in data.route" :key="s.n" class="step" :class="{ touched: s.touched, miss: s.miss, 'turn-back': s.turn_back }">
              <span class="marker" aria-hidden="true"></span>
              <div class="step-body">
                <p class="step-line">
                  <span class="muted small">{{ s.n }}.</span>
                  {{ verb(s) }}
                  <RouterLink v-if="s.kind === 'sign'" :to="{ name: 'sign', params: { id: s.target } }">{{ s.title }}</RouterLink>
                  <RouterLink v-else-if="s.kind === 'entry'" :to="{ name: 'entry', params: { id: s.target } }">{{ s.title }}</RouterLink>
                  <q v-else-if="s.target">{{ s.target }}</q>
                </p>
                <p class="badges">
                  <span v-if="s.turn_back" class="pill pill-turn">turned back</span>
                  <span v-if="s.miss" class="pill pill-stale">miss</span>
                  <RouterLink v-if="s.miss && s.gap_id" class="pill pill-gap" to="/gaps">gap {{ s.gap_id }}</RouterLink>
                  <span v-if="s.result.startsWith('refused')" class="pill pill-stale">{{ s.result }}</span>
                  <template v-if="s.touched"><span v-for="h in how.get(s.target)" :key="h" class="pill pill-touched">{{ h }} by this session</span></template>
                  <span class="muted small">{{ when(s.at) }}</span>
                </p>
                <p v-if="s.prerequisites.length" class="small">
                  Chain: also served
                  <template v-for="(p, i) in s.prerequisites" :key="p.id"><template v-if="i">, </template><RouterLink :to="{ name: 'entry', params: { id: p.id } }">{{ p.title }}</RouterLink></template>
                </p>
                <ul v-if="s.shown.length" class="plain small">
                  <li v-for="p in s.shown" :key="p.id">returned <RouterLink :to="{ name: 'entry', params: { id: p.id } }">{{ p.title }}</RouterLink></li>
                </ul>
              </div>
            </li>
          </ol>
        </section>

        <aside aria-labelledby="side-h">
          <section class="side-block">
            <h2 id="side-h">Off the trail</h2>
            <p v-if="!data.off_trail.length" class="muted small">No native reads were forwarded.</p>
            <ul class="plain">
              <li v-for="(o, i) in data.off_trail" :key="i" class="off-row">
                <code>{{ o.path }}</code>
                <span v-if="o.entry_id" class="small">anchored by <RouterLink :to="{ name: 'entry', params: { id: o.entry_id } }">{{ o.title }}</RouterLink></span>
                <span v-else class="muted small">no entry anchors this path</span>
              </li>
            </ul>
          </section>
          <section class="side-block">
            <h2>Written or confirmed</h2>
            <p v-if="!data.touched.length" class="muted small">None.</p>
            <ul class="plain">
              <li v-for="(t, i) in data.touched" :key="i" class="touched-row">
                <span class="pill pill-touched">{{ t.how }}</span>
                <RouterLink :to="{ name: 'entry', params: { id: t.id } }">{{ t.title }}</RouterLink>
                <TierBadge :tier="t.tier" />
              </li>
            </ul>
          </section>
        </aside>
      </div>
    </template>
  </Load>
</template>
