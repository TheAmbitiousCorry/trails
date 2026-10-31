import { ApiError } from './api'

export type Refusal = { rule: string; fix: string } | { message: string }

/** The server's refusal body is "rule: fix"; anything else, and a 404 (shown as `gone`), has no rule to name. */
export function refusal(e: unknown, gone = 'This no longer exists'): Refusal {
  if (e instanceof ApiError && e.status === 404) return { message: gone }
  const text = e instanceof Error ? e.message : String(e)
  const at = text.indexOf(': ')
  if (e instanceof ApiError && e.status === 400 && at > 0) return { rule: text.slice(0, at), fix: text.slice(at + 2) }
  return { message: text }
}
