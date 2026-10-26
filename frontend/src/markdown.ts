import { marked } from 'marked'
import DOMPurify from 'dompurify'

// Raw HTML in a body is dropped by the sanitiser; links open elsewhere and carry no opener.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer')
  }
})

export function renderMarkdown(src: string): string {
  const html = marked.parse(src, { async: false, gfm: true }) as string
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true }, FORBID_TAGS: ['style', 'form', 'input'] })
}
