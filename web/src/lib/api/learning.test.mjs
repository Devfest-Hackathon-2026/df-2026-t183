import assert from 'node:assert/strict'
import test from 'node:test'
import { isSummary, synthesizeSpeech, transcribeSpeech } from './learning.ts'

test('振り返りの空配列と正しい項目だけを受け付ける', () => {
  assert.equal(isSummary({ understood: [], review: [] }), true)
  assert.equal(isSummary({
    understood: [{ point: '光合成の材料', evidence: '二酸化炭素と水' }],
    review: [{ kind: 'unverified', text: '葉緑体のはたらき' }],
  }), true)
  assert.equal(isSummary({ understood: null, review: [] }), false)
  assert.equal(isSummary({ understood: [], review: [{ kind: 'unknown', text: '要確認' }] }), false)
  assert.equal(isSummary({ understood: [{ point: '材料', evidence: '水' }], review: [] }), false)
})

test('読み上げは質問文だけを送って MP3 を受け取る', async (t) => {
  const calls = []
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    calls.push({ url, options })
    return new Response(new Blob(['mp3']), { status: 200, headers: { 'Content-Type': 'audio/mpeg', 'Content-Length': '3' } })
  })
  const audio = await synthesizeSpeech('配列とは？', new AbortController().signal)
  assert.equal(await audio.text(), 'mp3')
  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, '/api/speech/synthesize')
  assert.equal(calls[0].options.method, 'POST')
  assert.equal(calls[0].options.headers['Content-Type'], 'application/json')
  assert.equal(calls[0].options.body, JSON.stringify({ text: '配列とは？' }))
  assert.equal(calls[0].options.credentials, 'omit')
  assert.equal(calls[0].options.cache, 'no-store')
})

test('読み上げは不正な音声応答を拒否する', async (t) => {
  for (const response of [
    new Response('oops', { status: 200, headers: { 'Content-Type': 'text/plain' } }),
    new Response('', { status: 200, headers: { 'Content-Type': 'audio/mpeg' } }),
    new Response('data', { status: 200, headers: { 'Content-Type': 'audio/mpeg', 'Content-Length': String(2 * 1024 * 1024 + 1) } }),
  ]) {
    t.mock.method(globalThis, 'fetch', async () => response)
    await assert.rejects(synthesizeSpeech('質問？', new AbortController().signal), /音声を確認できませんでした/)
    t.mock.restoreAll()
  }
})

test('読み上げAPIの障害は文字で続けられるエラーにする', async (t) => {
  for (const [status, message] of [
    [400, '質問を確認'], [401, '認証を確認'], [404, 'APIが見つかりません'],
    [413, '上限を超え'], [415, '送信形式'], [429, '混み合っています'],
    [500, '設定を確認'], [502, '読み上げに失敗'], [503, '利用できません'], [504, '時間がかかっています'],
  ]) {
    t.mock.method(globalThis, 'fetch', async () => new Response('', { status }))
    await assert.rejects(synthesizeSpeech('質問？', new AbortController().signal), new RegExp(message))
    t.mock.restoreAll()
  }
  t.mock.method(globalThis, 'fetch', async () => { throw new Error('private network detail') })
  await assert.rejects(synthesizeSpeech('質問？', new AbortController().signal), /読み上げAPIに接続できませんでした/)
})

import { startLearning, continueLearning, summarizeLearning, openHistory } from './learning.ts'
const sessionId = '2417ac2f-3cd3-4a18-8dd9-82a0205e5912'
const sessionProof = `s1.test.9999999999.${'a'.repeat(43)}`
const session = { sessionId, sessionProof, learningContent: '教材', history: [
  { role: 'model', content: 'どんな内容？' }, { role: 'user', content: '値を順番にまとめます' }, { role: 'model', content: 'なぜ並べるの？' },
] }
const saved = { sessionId, summary: { understood: [], review: [] }, saved: true, viewKey: '7KQM-3XWD-9HTR', savedAt: '2026-11-09T00:00:00Z', expiresAt: '2026-12-09T00:00:00Z' }
const signal = () => new AbortController().signal

test('Phase 2 API は署名を送信し、確定保存だけを成功として受け付ける', async t => {
  const calls = []; let response
  t.mock.method(globalThis, 'fetch', async (url, init) => {
    calls.push({ url, ...init, body: JSON.parse(init.body) })
    return new Response(JSON.stringify(response), { status: 200 })
  })
  response = { sessionId, reply: 'どんな内容？', sessionProof, sessionExpiresAt: new Date(9999999999 * 1000).toISOString() }
  assert.equal((await startLearning('教材', signal())).sessionProof, sessionProof)
  response = { reply: 'なぜそう思う？', sessionProof }
  assert.equal((await continueLearning(session, '回答', signal())).sessionProof, sessionProof)
  assert.equal(calls.at(-1).body.sessionProof, sessionProof)
  response = saved
  assert.deepEqual(await summarizeLearning(session, signal()), saved)
  const first = calls.at(-1).body
  await summarizeLearning(session, signal())
  assert.deepEqual(calls.at(-1).body, first)
  response = { ...saved, saved: false }
  await assert.rejects(summarizeLearning(session, signal()))
  response = { ...saved, summary: { understood: [{ point: '内容', evidence: 'これは会話に存在しません' }], review: [] } }
  await assert.rejects(summarizeLearning(session, signal()))
  response = { ...saved, learningContent: session.learningContent, history: session.history }
  await openHistory('test-key', signal())
  assert.equal(calls.at(-1).url, '/api/history/open')
  assert.deepEqual(calls.at(-1).body, { viewKey: 'test-key' })
  assert.equal(calls.at(-1).cache, 'no-store')
})
test('開始の署名と期限が両方ない場合だけ従来の API で続ける', async t => {
  let response = { sessionId, reply: 'どんな内容？' }
  const calls = []
  t.mock.method(globalThis, 'fetch', async (_, init) => {
    calls.push(JSON.parse(init.body))
    return new Response(JSON.stringify(response))
  })
  assert.deepEqual(await startLearning('教材', signal()), response)
  const legacy = { sessionId, learningContent: session.learningContent, history: session.history }
  assert.deepEqual(await continueLearning(legacy, '説明', signal()), { reply: response.reply })
  assert.equal(Object.hasOwn(calls.at(-1), 'sessionProof'), false)
  await assert.rejects(continueLearning(session, '説明', signal()))
  response = { ...response, sessionProof }
  await assert.rejects(startLearning('教材', signal()))
  response = { sessionId, summary: { understood: [], review: [] } }
  assert.deepEqual(await summarizeLearning(legacy, signal()), { ...response, saved: false })
  await assert.rejects(summarizeLearning(session, signal()))
})


test('音声APIは取消エラーを日本語化してAbortErrorを保つ', async t => {
  for (const [call, message] of [
    [signal => synthesizeSpeech('質問？', signal), '読み上げを取り消しました。'],
    [signal => transcribeSpeech(new Blob(['audio']), 'audio/webm', signal), '文字起こしを取り消しました。'],
  ]) {
    const controller = new AbortController()
    t.mock.method(globalThis, 'fetch', async () => {
      controller.abort()
      throw new DOMException('This operation was aborted', 'AbortError')
    })
    await assert.rejects(call(controller.signal), error => error.name === 'AbortError' && error.message === message)
    t.mock.restoreAll()
  }
})

test('音声APIは本文受信中の取消と読み上げの期限超過も日本語で扱う', async t => {
  const controller = new AbortController()
  t.mock.method(globalThis, 'fetch', async () => ({
    ok: true, headers: new Headers({ 'Content-Type': 'audio/mpeg' }),
    async blob() { controller.abort(); throw new DOMException('Aborted', 'AbortError') },
  }))
  await assert.rejects(synthesizeSpeech('質問？', controller.signal), { name: 'AbortError', message: '読み上げを取り消しました。' })
  t.mock.restoreAll()
  t.mock.timers.enable({ apis: ['setTimeout'] })
  t.mock.method(globalThis, 'fetch', async () => ({
    ok: true, headers: new Headers({ 'Content-Type': 'audio/mpeg' }),
    async blob() { t.mock.timers.tick(30_000); return new Blob(['mp3']) },
  }))
  await assert.rejects(synthesizeSpeech('質問？', signal()), /読み上げに時間がかかっています/)
})

test('文字起こしAPIのHTTPエラーは音声用の日本語で案内する', async t => {
  for (const [status, message] of [[400, '音声から言葉'], [401, '認証を確認'], [404, 'APIが見つかりません'], [413, '録音が上限'], [415, '録音形式'], [429, '混み合っています']]) {
    t.mock.method(globalThis, 'fetch', async () => new Response('', { status }))
    await assert.rejects(transcribeSpeech(new Blob(['audio']), 'audio/webm', signal()), new RegExp(message))
    t.mock.restoreAll()
  }
})


test('文字起こしはJSON受信と同時の期限超過を成功にしない', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  t.mock.method(globalThis, 'fetch', async () => ({
    ok: true,
    async json() { t.mock.timers.tick(55_000); return { text: '期限後の認識結果' } },
  }))
  await assert.rejects(transcribeSpeech(new Blob(['audio']), 'audio/webm', signal()), /文字起こしに時間がかかっています/)
})
