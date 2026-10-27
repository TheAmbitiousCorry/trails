// Hover and focus text for tier and kind marks, applied through the data-tip attribute (see style.css).
export const TIP: Record<string, string> = {
  verified: 'Verified: confirmed by two separate agent sessions that read it in full, or written by a person. Ranked first in search.',
  provisional: 'Provisional: written by an agent and not yet confirmed. Ranked below verified. Unread for 30 days it goes dormant.',
  stale: 'Stale: at least one anchor failed its check, such as a file that changed, a range that moved or an uncommitted edit. Still readable, but the agent is told to check the source. Its old tier returns when every anchor passes again.',
  dormant: 'Dormant: superseded, or provisional and unread for 30 days. Out of search and off the map, reachable by id.',
  gaps: 'Open gaps: searches from this section that found nothing. Each one is a question the wiki could not answer and should be written up or dismissed.',
  documents: 'Documents hanging directly under this section.',
  sections: 'Child sections under this one.',
  summary: 'Summary: what an area owns, why it exists and what it must never do. Up to 300 words, anchored to a file, directory or commit.',
  decision: 'Decision: who decided, when, why, and what was rejected. Up to 200 words.',
  procedure: 'Procedure: numbered steps to do one recurring task, such as build, release, restore or add a resource. Up to 400 words, anchored to the files the steps touch.',
  example: 'Example: one sentence of context plus a code range rendered live from the file, so it goes stale the moment the code moves.',
  reference: 'Reference: a pointer to a long document outside the repo. Up to 100 words with one URL.',
  render: 'Render: a screenshot, story URL or diagram, with the command that produced it and the commit it was produced at.',
  readwhen: 'Read when: the questions this document answers. An agent decides from this line whether to open it.',
}

// Attributes for an element that shows the tip for key, empty when the key has none.
export function tip(key: string): Record<string, string> {
  const text = TIP[key]
  return text ? { 'data-tip': text, tabindex: '0' } : {}
}
