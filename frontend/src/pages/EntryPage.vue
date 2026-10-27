<script setup lang="ts">
import { useRoute } from 'vue-router'
import { useApi, when, type EntryResponse } from '../api'
import Load from '../components/Load.vue'
import Markdown from '../components/Markdown.vue'
import SnippetBlock from '../components/SnippetBlock.vue'
import TierBadge from '../components/TierBadge.vue'

const route = useRoute()
const { data, error, loading } = useApi<EntryResponse>(() => `/api/entries/${encodeURIComponent(String(route.params.id))}`)
</script>

<template>
  <Load :ready="!!data" :loading="loading" :error="error">
    <template v-if="data">
      <nav class="crumbs" aria-label="Path">
        <template v-for="(p, i) in data.path" :key="p.id">
          <span v-if="i" aria-hidden="true">/</span>
          <RouterLink :to="{ name: 'sign', params: { id: p.id } }">{{ p.title }}</RouterLink>
        </template>
      </nav>
      <h1>{{ data.entry.title }}</h1>
      <p class="badges">
        <TierBadge :tier="data.entry.tier" />
        <span class="pill pill-kind">{{ data.entry.kind }}</span>
        <span v-if="data.entry.chain_length" class="pill pill-kind">chain +{{ data.entry.chain_length }}</span>
        <span class="muted small">{{ data.entry.reads_30d }} reads in 30 days, updated {{ when(data.entry.updated_at) }}</span>
      </p>
      <p class="lede">{{ data.entry.read_when }}</p>
      <p><RouterLink :to="{ path: '/graph', query: { focus: data.entry.id, depth: '1' } }">Show on graph</RouterLink></p>
      <p v-if="data.entry.tags.length" class="chips"><span v-for="t in data.entry.tags" :key="t" class="chip">{{ t }}</span></p>
      <p v-if="data.decided_by" class="muted">Decided by {{ data.decided_by }} on {{ data.decided_at }}.</p>
      <p v-if="data.superseded_by" class="notice">Superseded by <RouterLink :to="{ name: 'entry', params: { id: data.superseded_by } }">{{ data.superseded_by }}</RouterLink>.</p>
      <p v-if="data.entry.body_problem" class="notice error">{{ data.entry.body_problem }}</p>
      <Markdown v-else :source="data.entry.body_markdown" />
      <SnippetBlock v-for="a in data.entry.anchors.filter((x) => x.type === 'range' || x.type === 'file')" :key="a.id" :entry-id="data.entry.id" :anchor-id="a.id" />

      <section v-if="data.entry.anchors.length" aria-labelledby="anchors">
        <h2 id="anchors">Anchors</h2>
        <ul class="anchors">
          <li v-for="a in data.entry.anchors" :key="a.id" :class="{ failed: !a.ok }">
            <span class="pill pill-kind">{{ a.type }}</span>
            <code>{{ a.path_or_url }}<template v-if="a.type === 'range'">:{{ a.range_start }}-{{ a.range_end }}</template></code>
            <span class="muted small">checked {{ when(a.last_checked_at ?? '') }}</span>
            <span v-if="!a.ok" class="pill pill-stale">failed</span>
          </li>
        </ul>
      </section>

      <section v-if="data.links.length || data.backlinks.length" aria-labelledby="links">
        <h2 id="links">Links</h2>
        <ul class="plain">
          <li v-for="l in data.links" :key="`l${l.relation}${l.id}`"><span class="pill pill-kind">{{ l.relation }}</span> <RouterLink :to="{ name: 'entry', params: { id: l.id } }">{{ l.title }}</RouterLink> <TierBadge :tier="l.tier" /></li>
          <li v-for="l in data.backlinks" :key="`b${l.relation}${l.id}`"><span class="pill pill-kind">{{ l.relation }} this</span> <RouterLink :to="{ name: 'entry', params: { id: l.id } }">{{ l.title }}</RouterLink> <TierBadge :tier="l.tier" /></li>
        </ul>
      </section>

      <section aria-labelledby="versions">
        <h2 id="versions">Versions</h2>
        <ul class="plain">
          <li v-for="v in data.versions" :key="v.id">{{ when(v.created_at) }} <span class="muted">by</span> <code>{{ v.actor }}</code></li>
        </ul>
      </section>
    </template>
  </Load>
</template>
