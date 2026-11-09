import assert from 'node:assert/strict'
import test from 'node:test'
import { loadLearning, removeLearning, saveLearning, validState, STORAGE_KEY } from './session.ts'
const now = Date.parse('2026-11-09T00:00:00Z')
const expiresAt = '2026-11-09T02:00:00Z'
const state = () => ({ version: 1, expiresAt, draft: '途中の回答', checked: [true, false, false], frozen: false, session: {
  sessionId: '2417ac2f-3cd3-4a18-8dd9-82a0205e5912', sessionProof: `s1.test.${Date.parse(expiresAt) / 1000}.${'a'.repeat(43)}`,
  learningContent: '教材', history: [{ role: 'model', content: 'どんな内容？' }],
} })
function storage() { const map = new Map(); return { getItem: k => map.get(k) ?? null, setItem: (k,v) => map.set(k,v), removeItem: k => map.delete(k) } }
test('教材・確定履歴・署名・期限・下書きを一緒に復元する', () => {
  const port = storage(); assert.equal(saveLearning(port, state()), true)
  assert.deepEqual(loadLearning(port, now), state())
  assert.equal(removeLearning(port), true); assert.equal(port.getItem(STORAGE_KEY), null)
})
test('壊れた状態・期限切れ・不明版・不整合な署名期限を破棄する', () => {
  for (const item of ['{', '{}', JSON.stringify({ ...state(), version: 2 }), JSON.stringify({ ...state(), expiresAt: '2026-11-09T01:00:00Z' })]) {
    const port = storage(); port.setItem(STORAGE_KEY, item)
    assert.equal(loadLearning(port, now), undefined); assert.equal(port.getItem(STORAGE_KEY), null)
  }
  const port = storage(); saveLearning(port, state())
  assert.equal(loadLearning(port, Date.parse(expiresAt)), undefined)
})
test('保存結果と閲覧キー、余分なフィールドは永続化しない', () => {
  const port = storage(); saveLearning(port, { ...state(), viewKey: 'never-persist', summary: { understood: [], review: [] } })
  assert.equal(port.getItem(STORAGE_KEY).includes('never-persist'), false)
  assert.equal(port.getItem(STORAGE_KEY).includes('summary'), false)
})
test('確定前の履歴と凍結状態を検証し、再試行状態を復元する', () => {
  const s = state(); s.frozen = true; assert.equal(validState(s, now), false)
  s.session.history.push({ role: 'user', content: '教材を説明した内容です' }, { role: 'model', content: 'なぜそう思う？' })
  const port = storage(); saveLearning(port, s); assert.deepEqual(loadLearning(port, now), s)
  s.session.history.reverse(); s.session.history[1].role = 'model'; assert.equal(validState(s, now), false)
})
test('Storage が拒否された場合も例外を漏らさない', () => {
  const port = { getItem() { throw Error() }, setItem() { throw Error() }, removeItem() { throw Error() } }
  assert.equal(loadLearning(port, now), undefined); assert.equal(saveLearning(port, state()), false); assert.equal(removeLearning(port), false)
})

import { loadMaterial, saveMaterial, removeMaterial, DRAFT_KEY } from './session.ts'
test('開始前教材の固定期限を保ち、期限切れや破損を削除する', () => {
  const port = storage(); const deadline = now + 7200000
  assert.equal(saveMaterial(port, '教材の下書き', deadline), true)
  assert.deepEqual(loadMaterial(port, now), { text: '教材の下書き', expiresAt: deadline })
  assert.equal(loadMaterial(port, deadline), undefined)
  assert.equal(port.getItem(DRAFT_KEY), null)
  port.setItem(DRAFT_KEY, '{broken')
  assert.equal(loadMaterial(port, now), undefined)
  assert.equal(port.getItem(DRAFT_KEY), null)
  saveMaterial(port, '教材', deadline); assert.equal(removeMaterial(port), true)
  assert.equal(loadMaterial(port, now), undefined)
})
