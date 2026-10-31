<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'
import { ApiError, postJSON, projectApi, when, type AnchorForm, type EntryResponse } from '../api'
import { refusal, type Refusal } from '../refusal'
import Markdown from './Markdown.vue'
import RefusalNotice from './RefusalNotice.vue'
import RowList from './RowList.vue'

const props = defineProps<{ data: EntryResponse }>()
const emit = defineEmits<{ saved: [EntryResponse]; cancel: [] }>()

const anchorTypes = ['file', 'range', 'dir', 'url', 'commit', 'produced_by', 'transcript']
const uid = useId()
const f = (name: string) => `${uid}-${name}`

function draftOf(d: EntryResponse) {
  return {
    title: d.entry.title,
    read_when: d.entry.read_when,
    body: d.entry.body_markdown,
    tags: d.entry.tags.join(', '),
    anchors: d.form_anchors.map((a) => ({ ...a })) as AnchorForm[],
    requires: [...d.requires],
    see_also: [...d.see_also],
    decided_by: d.decided_by,
    decided_at: d.decided_at,
  }
}

// base is the entry the draft was started from; after a 409 the writer can start again from the newer one.
const base = ref(props.data)
const draft = ref(draftOf(props.data))
const etag = ref(props.data.etag)
const newer = ref<EntryResponse | null>(null)
const failed = ref<Refusal | null>(null)
const saving = ref(false)
const choice = ref<'replace' | 'distinct'>('replace')
const reason = ref('')
const alertBox = ref<InstanceType<typeof RefusalNotice> | null>(null)
const conflictBox = ref<HTMLElement | null>(null)

const isDecision = props.data.entry.kind === 'decision'
const signPath = props.data.path.map((p) => p.title).join(' / ')

function anchorOut(a: AnchorForm): AnchorForm {
  switch (a.type) {
    case 'file': case 'dir': case 'transcript': return { type: a.type, path: a.path ?? '' }
    case 'range': return { type: a.type, path: a.path ?? '', start: a.start ?? 0, end: a.end ?? 0 }
    case 'url': return { type: a.type, url: a.url ?? '' }
    case 'commit': return { type: a.type, sha: a.sha ?? '' }
    default: return { type: a.type, command: a.command ?? '', sha: a.sha }
  }
}

const ids = (xs: string[]) => xs.map((x) => x.trim()).filter(Boolean)

async function save() {
  if (saving.value) return
  saving.value = true
  // The choice rides on every save while the duplicate panel is open, so Enter in its reason field and the main Save button send it too.
  const resolve = isDuplicate()
  failed.value = null
  const d = draft.value
  try {
    const out = await postJSON<EntryResponse>(projectApi(`/entries/${encodeURIComponent(base.value.entry.id)}`), {
      etag: etag.value,
      title: d.title,
      read_when: d.read_when,
      body_markdown: d.body,
      tags: d.tags.split(',').map((t) => t.trim()).filter(Boolean),
      anchors: d.anchors.map(anchorOut),
      requires: ids(d.requires),
      see_also: ids(d.see_also),
      decided_by: isDecision ? d.decided_by : '',
      decided_at: isDecision ? d.decided_at : '',
      ...(resolve ? { resolution: choice.value, distinct_reason: choice.value === 'distinct' ? reason.value : '' } : {}),
    })
    emit('saved', out)
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      try {
        newer.value = JSON.parse(e.message) as EntryResponse
        await nextTick()
        conflictBox.value?.focus()
        return
      } catch {
        // A 409 without the newer entry falls through to the plain message.
      }
    }
    failed.value = refusal(e, 'This entry no longer exists')
    await nextTick()
    alertBox.value?.focus()
  } finally {
    saving.value = false
  }
}

function startAgain() {
  if (!newer.value) return
  base.value = newer.value
  draft.value = draftOf(newer.value)
  etag.value = newer.value.etag
  newer.value = null
  failed.value = null
}

const isDuplicate = () => !!failed.value && 'rule' in failed.value && failed.value.rule === 'duplicate'
</script>

<template>
  <div class="edit-wrap">
    <form class="edit-form" aria-label="Edit entry" @submit.prevent="save()">
      <dl class="readonly">
        <div><dt>Kind</dt><dd>{{ base.entry.kind }}</dd></div>
        <div><dt>Sign</dt><dd>{{ signPath }}</dd></div>
      </dl>

      <label :for="f('title')">Title</label>
      <input :id="f('title')" v-model="draft.title" type="text" required autocomplete="off" />

      <label :for="f('read')">Read when</label>
      <input :id="f('read')" v-model="draft.read_when" type="text" required autocomplete="off" />

      <div class="body-pair">
        <div class="body-edit">
          <label :for="f('body')">Body (Markdown)</label>
          <textarea :id="f('body')" v-model="draft.body" rows="16" required spellcheck="true"></textarea>
        </div>
        <div class="body-preview">
          <h2 :id="f('preview')" class="section-label">Preview</h2>
          <div :aria-labelledby="f('preview')" role="region"><Markdown :source="draft.body" /></div>
        </div>
      </div>

      <label :for="f('tags')">Tags (comma separated)</label>
      <input :id="f('tags')" v-model="draft.tags" type="text" autocomplete="off" spellcheck="false" />

      <template v-if="isDecision">
        <label :for="f('by')">Decided by</label>
        <input :id="f('by')" v-model="draft.decided_by" type="text" required autocomplete="off" />
        <label :for="f('at')">Decided at</label>
        <input :id="f('at')" v-model="draft.decided_at" type="text" required autocomplete="off" />
      </template>

      <fieldset class="rowlist">
        <legend>Anchors</legend>
        <p v-if="!draft.anchors.length" class="muted small">None.</p>
        <div v-for="(a, i) in draft.anchors" :key="i" class="anchor-row" role="group" :aria-label="`Anchor ${i + 1}`">
          <label :for="f(`at${i}`)">Type</label>
          <select :id="f(`at${i}`)" v-model="a.type">
            <option v-for="t in anchorTypes" :key="t" :value="t">{{ t }}</option>
          </select>
          <template v-if="['file', 'dir', 'range', 'transcript'].includes(a.type)">
            <label :for="f(`ap${i}`)">Path</label>
            <input :id="f(`ap${i}`)" v-model="a.path" type="text" autocomplete="off" spellcheck="false" />
          </template>
          <template v-if="a.type === 'range'">
            <label :for="f(`as${i}`)">First line</label>
            <input :id="f(`as${i}`)" v-model.number="a.start" type="number" min="1" />
            <label :for="f(`ae${i}`)">Last line</label>
            <input :id="f(`ae${i}`)" v-model.number="a.end" type="number" min="1" />
          </template>
          <template v-if="a.type === 'url'">
            <label :for="f(`au${i}`)">URL</label>
            <input :id="f(`au${i}`)" v-model="a.url" type="text" autocomplete="off" spellcheck="false" />
          </template>
          <template v-if="a.type === 'commit'">
            <label :for="f(`ac${i}`)">Commit</label>
            <input :id="f(`ac${i}`)" v-model="a.sha" type="text" autocomplete="off" spellcheck="false" />
          </template>
          <template v-if="a.type === 'produced_by'">
            <label :for="f(`am${i}`)">Command</label>
            <input :id="f(`am${i}`)" v-model="a.command" type="text" autocomplete="off" spellcheck="false" />
          </template>
          <button type="button" class="btn btn-quiet" :aria-label="`Remove anchor ${i + 1}`" @click="draft.anchors.splice(i, 1)">Remove</button>
        </div>
        <button type="button" class="btn btn-quiet" @click="draft.anchors.push({ type: 'file', path: '' })">Add anchor</button>
      </fieldset>

      <RowList v-model="draft.requires" label="Requires" add-label="Add required entry" placeholder="entry id" />
      <RowList v-model="draft.see_also" label="See also" add-label="Add related entry" placeholder="entry id" />

      <p v-if="base.staged_copies.length" class="notice warn">
        <template v-for="(c, i) in base.staged_copies" :key="c.id"><template v-if="i">, </template><strong>{{ c.branch }}</strong></template>
        holds a staged copy of this entry. Saving here does not change that copy: when its branch lands, the copy comes back as a pair to resolve instead of replacing this entry.
      </p>

      <fieldset v-if="isDuplicate()" class="dup-choice">
        <legend>This reads like an entry that already exists</legend>
        <label class="move-item"><input v-model="choice" type="radio" value="replace" />Replace it: the matching entry is superseded by this one</label>
        <label class="move-item"><input v-model="choice" type="radio" value="distinct" />Keep both: this is a different entry</label>
        <template v-if="choice === 'distinct'">
          <label :for="f('why')">Why it is distinct</label>
          <input :id="f('why')" v-model="reason" type="text" required autocomplete="off" />
        </template>
        <button type="button" class="btn" :disabled="saving" @click="save()">Save with this choice</button>
      </fieldset>

      <RefusalNotice v-if="failed" ref="alertBox" :value="failed" />

      <div class="form-actions">
        <button type="submit" class="btn" :disabled="saving">Save</button>
        <button type="button" class="btn btn-quiet" @click="emit('cancel')">Cancel</button>
        <span class="muted small">Saving changes the text and keeps the entry's tier; Confirm is how you vouch for it.</span>
      </div>
    </form>

    <aside v-if="newer" ref="conflictBox" class="newer" role="alert" tabindex="-1" aria-labelledby="newer-h">
      <h2 id="newer-h">This entry changed while you were editing</h2>
      <p>Your draft is kept on the left. The newer entry, saved {{ when(newer.entry.updated_at) }}, is below.</p>
      <p><button type="button" class="btn" @click="startAgain">Start again from the newer text</button></p>
      <h3>{{ newer.entry.title }}</h3>
      <p class="lede">{{ newer.entry.read_when }}</p>
      <p v-if="newer.entry.tags.length" class="chips"><span v-for="t in newer.entry.tags" :key="t" class="chip">{{ t }}</span></p>
      <p v-if="newer.entry.body_problem" class="notice error">{{ newer.entry.body_problem }}</p>
      <Markdown v-else :source="newer.entry.body_markdown" />
    </aside>
  </div>
</template>
