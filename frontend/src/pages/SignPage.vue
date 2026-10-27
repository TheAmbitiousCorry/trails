<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, postJSON, useApi, type Edge, type SignResponse } from '../api'
import { invalidate } from '../graph'
import Load from '../components/Load.vue'
import Markdown from '../components/Markdown.vue'
import SnippetBlock from '../components/SnippetBlock.vue'
import TierBadge from '../components/TierBadge.vue'

const route = useRoute()
const router = useRouter()
const { data, error, loading } = useApi<SignResponse>(() => `/api/signs/${encodeURIComponent(String(route.params.id))}`)

const newTitle = ref('')
const moveIds = ref<string[]>([])
const createError = ref<{ rule: string; fix: string } | { message: string } | null>(null)
const creating = ref(false)
watch(() => route.params.id, () => { newTitle.value = ''; createError.value = null; moveIds.value = [] })

const childSigns = computed(() => (data.value?.view.edges ?? []).filter((e) => e.type === 'sign' && e.id))
const directEntries = computed(() => (data.value?.groups ?? []).flatMap((g) => g.entries))

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
    const out = await postJSON<{ sign_id: string }>('/api/signs', { parent_id: String(route.params.id), title: newTitle.value, move: moveIds.value })
    await invalidate()
    await router.push({ name: 'sign', params: { id: out.sign_id } })
  } catch (e) {
    createError.value = refusal(e)
  } finally {
    creating.value = false
  }
}

function edgeCounts(e: Edge): string {
  if (e.type === 'sign') return `${e.child_count ?? 0} below${e.stale_count ? `, ${e.stale_count} stale` : ''}`
  if (e.type === 'entry') return `${e.kind}, ${e.word_count ?? 0} words`
  if (e.type === 'group') return `${e.member_count} entries`
  return ''
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
      <p><RouterLink :to="{ path: '/graph', query: { focus: data.view.sign_id, depth: '1' } }">Show on graph</RouterLink></p>
      <p class="badges">
        <span v-if="data.stale" class="chip chip-stale">{{ data.stale }} stale below</span>
        <span v-if="data.open_gaps" class="chip chip-gap">{{ data.open_gaps }} open {{ data.open_gaps === 1 ? 'gap' : 'gaps' }}</span>
      </p>

      <section aria-labelledby="edges">
        <h2 id="edges">Edges, as an agent sees them</h2>
        <p v-if="!data.view.edges.length" class="muted">This sign has no edges yet.</p>
        <ul class="edges">
          <li v-for="(e, i) in data.view.edges" :key="e.id ?? i" class="edge">
            <div class="edge-head">
              <RouterLink v-if="e.type === 'sign'" :to="{ name: 'sign', params: { id: e.id! } }">{{ e.title }}</RouterLink>
              <RouterLink v-else-if="e.type === 'entry'" :to="{ name: 'entry', params: { id: e.id! } }">{{ e.title }}</RouterLink>
              <a v-else-if="e.type === 'group'" :href="`#kind-${e.kind}`">{{ e.title }}</a>
              <span v-else>{{ e.title }}</span>
              <span class="chip chip-kind">{{ e.type }}</span>
              <TierBadge v-if="e.tier" :tier="e.tier" />
              <span class="muted small">{{ edgeCounts(e) }}</span>
            </div>
            <p v-if="e.read_when" class="read-when">{{ e.read_when }}</p>
            <p v-if="e.scent?.length" class="chips"><span v-for="s in e.scent" :key="s" class="chip">{{ s }}</span></p>
          </li>
        </ul>
        <p class="muted small">{{ data.view.lines }} lines in the agent's response.</p>
      </section>

      <section v-for="g in data.groups" :id="`kind-${g.kind}`" :key="g.kind" :aria-labelledby="`h-${g.kind}`">
        <h2 :id="`h-${g.kind}`">{{ g.kind }} <span class="muted small">{{ g.entries.length }}</span></h2>
        <article v-for="e in g.entries" :key="e.id" class="card">
          <header class="card-head">
            <h3><RouterLink :to="{ name: 'entry', params: { id: e.id } }">{{ e.title }}</RouterLink></h3>
            <TierBadge :tier="e.tier" />
            <span v-if="e.chain_length" class="chip chip-kind" :title="`Reading this serves ${e.chain_length} prerequisite ${e.chain_length === 1 ? 'entry' : 'entries'} first`">chain +{{ e.chain_length }}</span>
          </header>
          <p class="read-when">{{ e.read_when }}</p>
          <p v-if="e.tags.length" class="chips"><span v-for="t in e.tags" :key="t" class="chip">{{ t }}</span></p>
          <p v-if="e.body_problem" class="notice error">{{ e.body_problem }}</p>
          <Markdown v-else :source="e.body_markdown" />
          <template v-if="g.kind === 'example'">
            <SnippetBlock v-for="a in e.anchors.filter((x) => x.type === 'range' || x.type === 'file')" :key="a.id" :entry-id="e.id" :anchor-id="a.id" />
          </template>
          <ul v-if="e.anchors.length" class="anchors">
            <li v-for="a in e.anchors" :key="a.id" :class="{ failed: !a.ok }">
              <span class="chip chip-kind">{{ a.type }}</span>
              <code>{{ a.path_or_url }}<template v-if="a.type === 'range'">:{{ a.range_start }}-{{ a.range_end }}</template></code>
              <span v-if="!a.ok" class="chip chip-stale">failed</span>
            </li>
          </ul>
        </article>
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
