import test from 'node:test'
import assert from 'node:assert/strict'
import { polygonContains } from 'd3-polygon'
import { buildTree, foldGroups } from '../src/mapmodel.ts'
import { place } from '../src/mapLayout.ts'

const sign = (id, parent) => ({ type: 'sign', id, title: `${id}: about ${id}`, parent })
const entry = (id, signId, kind = 'rule') => ({ type: 'entry', id, title: `Title of ${id}`, kind, sign_id: signId, tier: 'verified', reads_30d: 0 })
const home = (nodes) => nodes.filter((n) => n.id !== 'root').map((n) => ({ from: n.type === 'sign' ? n.parent : n.sign_id, to: n.id, relation: 'home' }))

// root > a (entries a1 a2, sub-sign s with entry s1) and b (entry b1)
const nodes = [sign('root'), sign('a', 'root'), sign('b', 'root'), sign('s', 'a'), entry('a2', 'a'), entry('a1', 'a'), entry('s1', 's'), entry('b1', 'b')]
const tree = () => buildTree(nodes, home(nodes))

test('child signs come before entries in every fan', () => {
  const a = tree().children.find((n) => n.id === 'a')
  assert.deepEqual(a.children.map((n) => n.id), ['s', 'a1', 'a2'])
})

test('Ridge puts a sub-sign one column beyond its siblings and its entries one beyond that', () => {
  const pl = place('ridge', tree())
  const col = (id) => pl.byId.get(id).step
  assert.deepEqual(['a', 'a1', 's', 's1'].map(col), [1, 2, 3, 4])
  const x = (id) => pl.byId.get(id).x
  assert.ok(x('a1') > x('a') && x('s') > x('a1') && x('s1') > x('s'))
})

test('a sub-sign trail runs through its siblings column before going straight on', () => {
  for (const kind of ['ridge', 'topo']) {
    const pl = place(kind, tree())
    const path = pl.link(pl.byId.get('a'), pl.byId.get('s'))
    assert.match(path, new RegExp(`L${pl.byId.get('s').x},${pl.byId.get('s').y}$`))
    assert.ok(path.includes('C') || path.includes('L'))
  }
})

test('Topo rings follow the step rule and each starts past the previous ring titles', () => {
  const pl = place('topo', tree())
  assert.deepEqual(['a', 'a1', 's', 's1'].map((id) => pl.byId.get(id).step), [1, 2, 3, 4])
  for (let r = 1; r < pl.rings.length; r++) assert.ok(pl.rings[r] > pl.rings[r - 1] + 100)
  assert.equal(pl.byId.get('s').radius, pl.rings[3])
})

test('more than twelve entries of one kind fold into one group, and a member resolves to it', () => {
  const many = [sign('root'), sign('a', 'root'), ...Array.from({ length: 13 }, (_, i) => entry(`e${i}`, 'a', 'example')), entry('r1', 'a', 'rule')]
  const folded = foldGroups(buildTree(many, home(many)))
  const a = folded.children[0]
  assert.deepEqual(a.children.map((n) => n.type), ['group', 'entry'])
  assert.equal(a.children[0].title, '13 example entries')
  const pl = place('topo', folded)
  assert.equal(pl.byId.get('e7'), pl.byId.get('a/example'))
  assert.equal(foldGroups(buildTree(many.slice(0, 14), home(many.slice(0, 14)))).children[0].children.length, 12)
})

test('Cluster keeps island hulls apart and the trailhead out of them', () => {
  const big = [sign('root')]
  for (const d of ['a', 'b', 'c', 'd']) {
    big.push(sign(d, 'root'), sign(`${d}s`, d))
    for (let i = 0; i < 9; i++) big.push(entry(`${d}${i}`, d), entry(`${d}s${i}`, `${d}s`))
  }
  const pl = place('cluster', buildTree(big, home(big)))
  const islands = pl.hulls.filter((h) => !h.peninsula)
  assert.equal(islands.length, 4)
  assert.equal(pl.hulls.length - islands.length, 4)
  for (const [i, h] of islands.entries()) {
    assert.ok(!polygonContains(h.points, [pl.byId.get('root').x, pl.byId.get('root').y]))
    for (const [j, other] of islands.entries()) if (i !== j) assert.ok(!h.points.some((p) => polygonContains(other.points, p)))
  }
  const again = place('cluster', buildTree(big, home(big)))
  assert.deepEqual(again.placed.map((p) => [p.x, p.y]), pl.placed.map((p) => [p.x, p.y]))
})
