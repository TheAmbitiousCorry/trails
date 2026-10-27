<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, postJSON, useApi, type SignResponse, projectApi } from '../api'
import { invalidate } from '../graph'
import Load from '../components/Load.vue'
import TierBadge from '../components/TierBadge.vue'
import { tip } from '../tips'

const route = useRoute()
const router = useRouter()
const { data, error, loading } = useApi<SignResponse>(() => projectApi(`/signs/${encodeURIComponent(String(route.params.id))}`))

const newTitle = ref('')
const moveIds = ref<string[]>([])
const createError = ref<{ rule: string; fix: string } | { message: string } | null>(null)
const creating = ref(false)
watch(() => route.params.id, () => { newTitle.value = ''; createError.value = null; moveIds.value = [] })

const childSigns = computed(() => (data.value?.view.edges ?? []).filter((e) => e.type === 'sign' && e.id))
const directEntries = computed(() => (data.value?.groups ?? []).flatMap((g) => g.entries))
const tierCounts = computed(() => {
  const out: Record<string, number> = {}
  for (const e of directEntries.value) out[e.tier] = (out[e.tier] ?? 0) + 1
  return out
})

// The server's refusal body is "rule: fix".
function refusal(e: unknown) {
  if (e instanceof ApiError && e.status === 404) return { message: 'This sign no longer exists' }
  const text = e instanceof Error ? e.message : String(e)
  const at = text.indexOf(': ')
  if (e instanceof ApiError && e.status === 400 && at > 0) return { rule: text.slice(0, at), fix: text.slice(at + 2) }
  return { message: text }
}

async function createSign() {
  if (creating.value) return
  creating.value = true
  createError.value = null
  try {
    const out = await postJSON<{ sign_id: string }>(projectApi('/signs'), { parent_id: String(route.params.id), title: newTitle.value, move: moveIds.value })
    await invalidate()
    await router.push({ name: 'sign', params: { id: out.sign_id } })
  } catch (e) {
    createError.value = refusal(e)
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <Load :ready="!!data" :loading="loading" :error="error">
    <template v-if="data">
      <nav class="crumbs" aria-label="Path">
        <template v-for="(p, i) in data.path" :key="p.id">
          <span v-if="i" aria-hidden="true">/</span>
          <RouterLink v-if="i < data.path.length - 1" :to="{ name: 'sign', params: { id: p.id } }">{{ p.title }}</RouterLink>
          <span v-else aria-current="page">{{ p.title }}</span>
        </template>
      </nav>
      <h1>{{ data.view.title }}</h1>
      <p class="lede">{{ data.view.summary }}</p>
      <p><RouterLink :to="{ name: 'graph', query: { focus: data.view.sign_id, depth: '1' } }">Show on graph</RouterLink></p>
      <p class="badges">
        <span class="chip" v-bind="tip('documents')">{{ directEntries.length }} {{ directEntries.length === 1 ? 'document' : 'documents' }}</span>
        <span class="chip" v-bind="tip('sections')">{{ childSigns.length }} {{ childSigns.length === 1 ? 'section' : 'sections' }}</span>
        <span v-if="tierCounts.verified" class="chip chip-verified" v-bind="tip('verified')">{{ tierCounts.verified }} verified</span>
        <span v-if="tierCounts.provisional" class="chip chip-provisional" v-bind="tip('provisional')">{{ tierCounts.provisional }} provisional</span>
        <span v-if="data.stale" class="chip chip-stale" v-bind="tip('stale')">{{ data.stale }} stale</span>
        <span v-if="data.open_gaps" class="chip chip-gap" v-bind="tip('gaps')">{{ data.open_gaps }} open {{ data.open_gaps === 1 ? 'gap' : 'gaps' }}</span>
      </p>

      <section v-if="childSigns.length" aria-labelledby="sections">
        <h2 id="sections" class="section-label">Sections</h2>
        <ul class="rows">
          <li v-for="c in childSigns" :key="c.id" class="row-item">
            <span class="row-mark"><span class="sq" aria-hidden="true"></span></span>
            <RouterLink class="row-title" :to="{ name: 'sign', params: { id: c.id! } }">{{ c.title }}</RouterLink>
            <span v-if="c.read_when" class="row-line">{{ c.read_when }}</span>
          </li>
        </ul>
      </section>

      <section aria-labelledby="documents">
        <h2 id="documents" class="section-label">Documents</h2>
        <p v-if="!directEntries.length" class="muted">No documents yet.</p>
        <ul v-else class="rows">
          <li v-for="e in directEntries" :key="e.id" class="row-item">
            <span class="row-mark"><TierBadge :tier="e.tier" dot-only /></span>
            <span class="row-head">
              <RouterLink class="row-title" :to="{ name: 'entry', params: { id: e.id } }">{{ e.title }}</RouterLink>
              <span class="row-kind" v-bind="tip(e.kind)">{{ e.kind }}</span>
            </span>
            <span class="row-line"><span class="rw-label" v-bind="tip('readwhen')">read when</span> {{ e.read_when }}</span>
          </li>
        </ul>
      </section>

      <section aria-labelledby="add-sign">
        <h2 id="add-sign">Add a sign under this one</h2>
        <form class="add-sign" @submit.prevent="createSign">
          <label for="new-sign-title" class="muted small">Title</label>
          <input id="new-sign-title" v-model="newTitle" type="text" maxlength="80" required autocomplete="off" :aria-describedby="createError ? 'new-sign-error' : undefined" :aria-invalid="!!createError" />
          <button type="submit" :disabled="creating">Add sign</button>
          <div v-if="createError" id="new-sign-error" class="refusal" role="alert">
            <template v-if="'rule' in createError"><strong>{{ createError.rule }}</strong>: {{ createError.fix }}</template>
            <template v-else>{{ createError.message }}</template>
          </div>
          <fieldset v-if="childSigns.length || directEntries.length" class="move-under">
            <legend class="muted small">Move under it</legend>
            <label v-for="c in childSigns" :key="c.id" class="move-item"><input v-model="moveIds" type="checkbox" :value="c.id" />{{ c.title }} <span class="chip chip-kind">sign</span></label>
            <label v-for="e in directEntries" :key="e.id" class="move-item"><input v-model="moveIds" type="checkbox" :value="e.id" />{{ e.title }} <span class="chip chip-kind">{{ e.kind }}</span></label>
          </fieldset>
        </form>
      </section>
    </template>
  </Load>
</template>
