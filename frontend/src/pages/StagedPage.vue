<script setup lang="ts">
import { ref } from 'vue'
import { ApiError, postJSON, useApi, when, projectApi, type StagedBranch, type SweepResult } from '../api'
import { invalidate } from '../graph'
import Load from '../components/Load.vue'
import TierBadge from '../components/TierBadge.vue'
import { tip } from '../tips'

const { data, error, loading, reload } = useApi<{ branches: StagedBranch[] }>(() => projectApi('/staged'))

const stateText = {
  branch_only: 'Work not committed yet, or committed in files this entry does not anchor.',
  committed: 'Work committed on the branch; waiting for it to reach the base branch.',
  abandoned: 'Branch deleted before its work landed. Dormant, readable by id.',
}

const busy = ref('')
const failure = ref('')
const note = ref('')

async function promote(id: string) {
  if (busy.value) return
  busy.value = id
  failure.value = ''
  note.value = ''
  try {
    const out = await postJSON<{ replaced: string }>(projectApi(`/entries/${encodeURIComponent(id)}/promote`), {})
    note.value = out.replaced ? `Promoted ${id}; it replaced ${out.replaced}.` : `Promoted ${id}.`
    await invalidate()
    reload()
  } catch (e) {
    failure.value = e instanceof ApiError && e.status === 404 ? 'That entry no longer exists.' : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = ''
  }
}

async function sweep() {
  if (busy.value) return
  busy.value = 'sweep'
  failure.value = ''
  note.value = ''
  try {
    const r = await postJSON<SweepResult>(projectApi('/sweep'), {})
    note.value = `Sweep done: ${r.promoted} promoted, ${r.abandoned} abandoned, ${r.checked} checked.` + (r.base_unresolved ? ' A base branch has no ref, so those entries were left as they are.' : '')
    await invalidate()
    reload()
  } catch (e) {
    failure.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <h1>Staged</h1>
  <p class="muted">Entries written about branch work, held on their branch until it lands on the base branch.</p>
  <p>
    <button type="button" class="supersede sweep-now" :disabled="!!busy" @click="sweep">Sweep now</button>
  </p>
  <p v-if="note" class="notice" role="status">{{ note }}</p>
  <p v-if="failure" class="notice error" role="alert">{{ failure }}</p>
  <Load :ready="!!data" :loading="loading" :error="error">
    <p v-if="data && !data.branches.length" class="muted">Nothing is staged. An entry about work a branch has changed shows up here until the work lands.</p>
    <section v-for="b in data?.branches" :key="b.branch" :aria-label="`Branch ${b.branch}`">
      <h2 class="branch-head">
        <code>{{ b.branch }}</code>
        <span class="chip chip-kind">base {{ b.base || 'unknown' }}</span>
        <span v-if="!b.alive" class="chip chip-stale">branch gone</span>
      </h2>
      <ul class="cards">
        <li v-for="e in b.entries" :key="e.id" class="card">
          <h3 class="card-title"><RouterLink :to="{ name: 'entry', params: { id: e.id } }">{{ e.title }}</RouterLink></h3>
          <p class="badges">
            <TierBadge :tier="e.tier" />
            <span class="chip chip-kind">{{ e.kind }}</span>
            <span class="chip" :class="e.state === 'abandoned' ? 'chip-stale' : 'chip-provisional'" v-bind="tip('staged')">{{ e.state.replace('_', ' ') }}</span>
            <span class="muted small">{{ stateText[e.state] }}</span>
          </p>
          <p class="badges small muted">
            <span v-if="e.landing">landing <code>{{ e.landing.slice(0, 10) }}</code></span>
            <span v-else>no landing commit</span>
            <span>last check {{ e.last_check ? when(e.last_check) : 'never' }}</span>
          </p>
          <p v-if="e.stages_over" class="small">Stage-over: replaces <RouterLink :to="{ name: 'entry', params: { id: e.stages_over.id } }">{{ e.stages_over.title }}</RouterLink> <TierBadge :tier="e.stages_over.tier" /> when it lands. A verified or changed original is not replaced; the pair is left for you to resolve.</p>
          <p><button type="button" class="supersede promote" :disabled="!!busy" :aria-label="`Promote ${e.title} by hand`" @click="promote(e.id)">Promote now</button></p>
        </li>
      </ul>
    </section>
  </Load>
</template>
