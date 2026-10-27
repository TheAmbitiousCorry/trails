<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { hopVerb, useApi, when, type EntryResponse, type GraphResponse, type RouteStep, type SessionResponse } from '../api'
import LayoutSwitch from '../components/LayoutSwitch.vue'
import Load from '../components/Load.vue'
import Markdown from '../components/Markdown.vue'
import TierBadge from '../components/TierBadge.vue'
import TrailMap, { type ReplayHop } from '../components/TrailMap.vue'
import { useLayout } from '../layoutChoice'

const HOP_MS = 650

const route = useRoute()
const router = useRouter()
const id = computed(() => encodeURIComponent(String(route.params.id)))
const { data, error, loading } = useApi<SessionResponse>(() => `/api/sessions/${id.value}`)
const walk = useApi<GraphResponse>(() => `/api/graph?session=${id.value}`)
const { layout, choose } = useLayout()

const how = computed(() => {
  const m = new Map<string, string[]>()
  for (const t of data.value?.touched ?? []) m.set(t.id, [...(m.get(t.id) ?? []), t.how])
  return m
})

function verb(s: RouteStep): string {
  return hopVerb(s.tool, s.depth)
}

// One replay hop per route row, so the scrubber position is also the row number; rows with no node (searches) draw nothing.
const hops = computed<ReplayHop[]>(() => {
  const byN = new Map((walk.data.value?.route ?? []).map((h) => [h.n, h]))
  return (data.value?.route ?? []).map((s) => {
    const h = byN.get(s.n)
    return { target: s.kind === 'query' ? '' : (h?.target ?? s.target), prerequisites: h?.prerequisites ?? s.prerequisites.map((p) => p.id), label: s.n }
  })
})
const total = computed(() => hops.value.length)

const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches
const at = ref(0)
const playing = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

function stop() {
  playing.value = false
  clearInterval(timer)
}
function select(n: number) {
  stop()
  at.value = Math.min(Math.max(n, 0), total.value)
}
function play() {
  if (reduced()) { select(total.value); return }
  if (at.value >= total.value) at.value = 0
  playing.value = true
  timer = setInterval(() => {
    if (at.value >= total.value) { stop(); return }
    at.value++
    if (at.value >= total.value) stop()
  }, HOP_MS)
}
function onScrub(ev: Event) {
  select(Number((ev.target as HTMLInputElement).value))
}

// A session opens on its finished walk; Play replays it from the trailhead.
watch(() => [data.value?.session.id, total.value], () => { stop(); at.value = total.value }, { immediate: true })
onBeforeUnmount(stop)

const step = computed<RouteStep | undefined>(() => (at.value > 0 ? data.value?.route[at.value - 1] : undefined))
const entryId = computed(() => (step.value?.kind === 'entry' ? step.value.target : null))
const entry = useApi<EntryResponse>(() => (entryId.value ? `/api/entries/${encodeURIComponent(entryId.value)}` : null))
const entryNow = computed(() => (entryId.value && entry.data.value?.entry.id === entryId.value ? entry.data.value : null))
const firstParagraph = computed(() => (entryNow.value?.entry.body_markdown ?? '').split(/\n\s*\n/).find((p) => p.trim()) ?? '')

const open = (nodeId: string) => {
  const sign = walk.data.value?.nodes.find((n) => n.id === nodeId)?.type === 'sign'
  void router.push(sign ? { name: 'sign', params: { id: nodeId } } : { name: 'entry', params: { id: nodeId } })
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

      <section class="replay" aria-labelledby="replay-h">
        <h2 id="replay-h">Walk</h2>
        <div class="controls replay-controls">
          <LayoutSwitch :layout="layout" @choose="choose" />
          <button type="button" class="play" :aria-pressed="playing" @click="playing ? stop() : play()">{{ playing ? 'Pause' : 'Play' }}</button>
          <label class="scrub">Hop
            <input type="range" min="0" :max="total" step="1" :value="at" aria-label="Hop in the walk" :aria-valuetext="`hop ${at} of ${total}`" @input="onScrub" />
          </label>
          <span class="muted small" data-testid="hop-count" aria-live="off">{{ at }} of {{ total }}</span>
        </div>
        <div class="replay-grid">
          <div class="replay-map">
            <TrailMap :layout="layout" :replay="hops" :at="at" :live="playing" :data="walk.data.value ?? undefined" @open="open" />
          </div>
          <div class="hop-pane" aria-live="polite" data-testid="hop-pane">
            <p v-if="!step" class="muted">Move the slider or press Play to walk the route from the trailhead.</p>
            <template v-else>
              <p class="muted small">Hop {{ step.n }}: {{ verb(step) }}</p>
              <p v-if="step.tool === 'session_end'">The session ended{{ data?.session.ended_by ? `, by ${data.session.ended_by}` : '' }}.</p>
              <template v-else-if="step.kind === 'query'">
                <p class="hop-title">Search text</p>
                <p><q>{{ step.target }}</q></p>
              </template>
              <template v-else-if="step.kind === 'sign'">
                <p class="hop-title"><RouterLink :to="{ name: 'sign', params: { id: step.target } }">{{ step.title }}</RouterLink></p>
              </template>
              <template v-else>
                <p v-if="entry.loading.value && !entryNow" class="muted">Loading</p>
                <p v-else-if="!entryNow" class="muted">{{ step.title }} could not be loaded.</p>
                <template v-else>
                  <p class="hop-title"><RouterLink :to="{ name: 'entry', params: { id: step.target } }">{{ entryNow.entry.title }}</RouterLink> <TierBadge :tier="entryNow.entry.tier" /></p>
                  <p v-if="entryNow.entry.read_when" class="small"><span class="muted">Read when</span> {{ entryNow.entry.read_when }}</p>
                  <Markdown :source="firstParagraph" />
                </template>
              </template>
            </template>
          </div>
        </div>
        <p class="muted small caption">Forks as the map is now</p>
      </section>

      <div class="trail-layout">
        <section aria-labelledby="route-h">
          <h2 id="route-h">Route</h2>
          <p v-if="!data.route.length" class="muted">This session made no calls after it started.</p>
          <ol class="timeline">
            <li v-for="s in data.route" :key="s.n" class="step" :class="{ current: at === s.n, ahead: s.n > at, touched: s.touched, miss: s.miss, 'turn-back': s.turn_back }">
              <span class="marker" aria-hidden="true"></span>
              <div class="step-body">
                <p class="step-line">
                  <button type="button" class="hop-select" :aria-current="at === s.n ? 'step' : undefined" @click="select(s.n)"><span class="muted small">{{ s.n }}.</span> {{ verb(s) }}</button>
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


<style scoped>
.replay-controls { align-items: center; }
.play { font: inherit; padding: 0.35rem 0.9rem; border: 1px solid var(--line); border-radius: 6px; background: var(--surface); color: var(--text); cursor: pointer; min-width: 5rem; }
.scrub { flex-direction: row !important; align-items: center; gap: 0.5rem !important; flex: 1 1 14rem; max-width: 30rem; }
.scrub input { flex: 1; min-width: 0; accent-color: var(--accent); }
.replay-grid { display: grid; gap: 1rem; grid-template-columns: minmax(0, 1fr); }
@media (min-width: 900px) { .replay-grid { grid-template-columns: minmax(0, 1fr) 20rem; align-items: start; } }
.replay-map { height: 60vh; min-height: 24rem; }
.hop-pane { background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius); padding: 0.2rem var(--gutter) 0.8rem; overflow-wrap: anywhere; }
.hop-title { font-weight: 600; margin-bottom: 0.3rem; }
.caption { margin: 0.4rem 0 1rem; }
.hop-select { font: inherit; color: inherit; background: none; border: 0; padding: 0; text-align: left; cursor: pointer; }
.hop-select::after { content: ''; position: absolute; inset: 0; }
.step :deep(a) { position: relative; z-index: 1; }
.step.current { outline: 2px solid var(--accent); outline-offset: -2px; }
.step.ahead .step-line { opacity: 0.6; }
</style>
