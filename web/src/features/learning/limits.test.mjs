import assert from 'node:assert/strict'
import test from 'node:test'
import { normalizeText, textLength } from './limits.ts'

test('教材と回答の文字数を Go と同じ Unicode 空白・コードポイントで数える', () => {
  assert.equal(normalizeText('\u0085 光\n合成 \u3000'), '光\n合成')
  assert.equal(textLength('\u3000 𠮷🌱\u00a0'), 2)
  assert.equal(textLength('\ufeff'), 1)
})
