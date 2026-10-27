import { ref, type Ref } from 'vue'
import type { LayoutKind } from './mapLayout'

export const LAYOUTS: { id: LayoutKind; label: string }[] = [
  { id: 'ridge', label: 'Ridge' },
  { id: 'topo', label: 'Topo' },
  { id: 'cluster', label: 'Cluster' },
]
const STORE_KEY = 'trails.graph.layout'

/** The layout every trail map page shares, remembered in the browser; Ridge until the viewer picks another. */
export function useLayout(): { layout: Ref<LayoutKind>; choose: (l: LayoutKind) => void } {
  let start: LayoutKind = 'ridge'
  try {
    const v = localStorage.getItem(STORE_KEY)
    if (LAYOUTS.some((l) => l.id === v)) start = v as LayoutKind
  } catch { /* storage can be blocked */ }
  const layout = ref<LayoutKind>(start)
  function choose(l: LayoutKind) {
    layout.value = l
    try { localStorage.setItem(STORE_KEY, l) } catch { /* storage can be blocked */ }
  }
  return { layout, choose }
}
