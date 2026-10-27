import test from 'node:test'
import assert from 'node:assert/strict'
import { forkProngs, FORK_CAP } from '../src/mapmodel.ts'

const sign = (id, parent) => ({ type: 'sign', id, title: id, parent })
const entry = (id, kind, signId) => ({ type: 'entry', id, title: id, kind, sign_id: signId, tier: 'verified', reads_30d: 0 })

test('a sign with two child signs and entries of three kinds yields five prongs', () => {
  const nodes = [
    sign('root'), sign('a', 'root'), sign('b', 'root'),
    ...['s1', 's2', 's3'].map((id) => entry(id, 'summary', 'root')),
    ...['d1', 'd2'].map((id) => entry(id, 'decision', 'root')),
    entry('x1', 'example', 'root'),
    entry('inner', 'rule', 'a'),
  ]
  const { prongs, more } = forkProngs(nodes, 'root')
  assert.equal(more, 0)
  assert.deepEqual(prongs.map((p) => [p.kind, p.count]), [['sign', 1], ['sign', 1], ['entries', 3], ['entries', 2], ['entries', 1]])
  assert.deepEqual(prongs[2].entryIds, ['s1', 's2', 's3'])
  assert.equal(prongs[4].id, 'x1')
})

test('a lone summary is free, so one summary and one decision yield one prong', () => {
  const nodes = [sign('root'), entry('s1', 'summary', 'root'), entry('d1', 'decision', 'root')]
  const { prongs } = forkProngs(nodes, 'root')
  assert.equal(prongs.length, 1)
  assert.equal(prongs[0].id, 'd1')
})

test('more than eight prongs are cut with the remainder counted', () => {
  const nodes = [sign('root'), ...Array.from({ length: 11 }, (_, i) => sign(`c${i}`, 'root'))]
  const { prongs, more } = forkProngs(nodes, 'root')
  assert.equal(prongs.length, FORK_CAP)
  assert.equal(more, 3)
})
