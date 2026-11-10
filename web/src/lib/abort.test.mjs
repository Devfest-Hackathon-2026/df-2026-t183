import assert from 'node:assert/strict'
import test from 'node:test'
import { abortable, deadline } from './abort.ts'

test('処理の終了を待たずに中断を返す', async () => {
  const controller = new AbortController()
  const reason = new DOMException('timeout', 'TimeoutError')
  const reading = abortable(controller.signal, () => new Promise(() => {}))
  controller.abort(reason)
  await assert.rejects(reading, (error) => error === reason)
})

test('中断済みの読み取りを開始しない', async () => {
  await assert.rejects(abortable(AbortSignal.abort(), async () => {
    assert.fail('中断後に読み取りが開始された')
  }), { name: 'AbortError' })
})

test('期限と親の中断を別々に扱う', async () => {
  const parent = new AbortController()
  const expired = deadline(parent.signal, 1)
  try {
    await assert.rejects(abortable(expired.signal, () => new Promise(() => {})), { name: 'TimeoutError' })
    assert.equal(expired.timedOut, true)
  } finally {
    expired.dispose()
  }

  const cancelled = deadline(parent.signal, 100)
  parent.abort()
  assert.equal(cancelled.signal.aborted, true)
  assert.equal(cancelled.timedOut, false)
  cancelled.dispose()
})
