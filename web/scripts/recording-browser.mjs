import assert from 'node:assert/strict'
import { realpathSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'

// PLAYWRIGHT_MODULE may point at an existing isolated Playwright installation.
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const root = fileURLToPath(new URL('../', import.meta.url))
const harness = '/__recording-test.tsx'
const harnessSource = `
import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Conversation } from '/src/features/learning/Conversation'
import { useSpeechPlayback } from '/src/features/learning/useSpeechPlayback'
import '/src/styles.css'
window.calls = []; window.sent = []; window.callbacks = [];
function Harness() {
  const [draft, setDraft] = useState('元の下書き');
  const [shown, setShown] = useState(true);
  const [question, setQuestion] = useState('説明してください');
  const [turn, setTurn] = useState(0);
  const speech = useSpeechPlayback(question, turn, shown);
  window.speech = speech;
  window.newQuestion = text => { setQuestion(text); setTurn(value => value + 1) };
  window.hide = () => setShown(false);
  window.show = () => setShown(true);
  window.unmount = () => window.testRoot.unmount();
  return shown && <Conversation history={[{role:'model',content:'説明してください'}]} material={{text:'教材'}} draft={draft} onDraft={setDraft}
    onSend={text => window.sent.push(text)} onCheck={() => {}}
    onTranscribe={async (blob, type, done) => { window.calls.push({text: await blob.text(),type}); window.callbacks.push(done) }}
    speech={speech} pending={false} pendingLabel="" error="" onCancelRequest={() => {}} />
}
window.testRoot = createRoot(document.getElementById('root'));
window.testRoot.render(<React.StrictMode><Harness /></React.StrictMode>);
`
const harnessPage = '/__recording-test.html'
const server = await createServer({
  cacheDir: root + '.vite-recording-test',
  root, server: { host: '127.0.0.1', port: 0, fs: { allow: [root, realpathSync(root + 'node_modules')] } },
  plugins: [{
    name: 'recording-test-harness',
    resolveId(id) { if (id === harness) return root + '__recording-test.tsx' },
    load(id) { if (id === root + '__recording-test.tsx') return harnessSource },
    configureServer(vite) {
      vite.middlewares.use(async (req, res, next) => {
        if (req.url?.split('?')[0] !== harnessPage) return next()
        const html = await vite.transformIndexHtml(harnessPage, `<div id="root"></div><script type="module" src="${harness}"></script>`)
        res.setHeader('Content-Type', 'text/html')
        res.end(html)
      })
    },
  }],
})
let browser
try {
  await server.listen()
  browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE })
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  const errors = []
  page.on('pageerror', error => (errors.push(error.message), console.error(error.message)))
  await page.addInitScript(() => {
    window.players = []; window.revoked = []; window.rejectPlay = false; window.deferPlay = false; window.pendingPlays = [];
    const revoke = URL.revokeObjectURL.bind(URL);
    URL.revokeObjectURL = url => { window.revoked.push(url); revoke(url) };
    window.Audio = class {
      constructor(src) { this.src = src; this.paused = true; this.currentTime = 0; this.plays = 0; window.players.push(this) }
      async play() {
        this.plays++; if (window.rejectPlay) throw new Error('autoplay');
        if (window.deferPlay) await new Promise(resolve => window.pendingPlays.push(resolve));
        this.paused = false; this.onplay?.();
      }
      pause() { this.paused = true; this.onpause?.() }
      removeAttribute(name) { if (name === 'src') this.src = '' }
      load() {}
    };
    window.streams = []; window.recorders = []; window.requests = []; window.deferMic = false; window.rejectMic = false;
    Object.defineProperty(navigator.mediaDevices, 'getUserMedia', { value: () => new Promise((resolve, reject) => {
      const track = { stopped: false, stop() { this.stopped = true } };
      const stream = { getTracks: () => [track] }; window.streams.push(stream);
      const finish = () => window.rejectMic ? reject(new Error('denied')) : resolve(stream);
      window.requests.push(finish); if (!window.deferMic) finish();
    }) });
    window.MediaRecorder = class {
      static isTypeSupported() { return true }
      constructor(stream, options) { this.stream = stream; this.mimeType = options.mimeType; this.state = 'inactive'; window.recorders.push(this) }
      start() { if (window.failStart) throw new Error('start'); this.state = 'recording' }
      stop() { this.state = 'inactive' }
      finish(text = '音声') { this.ondataavailable?.({ data: new Blob([text]) }); this.onstop?.() }
    };
  })
  const url = server.resolvedUrls.local[0].replace(/\/$/, '') + harnessPage
  async function reset() { await page.goto(url); await page.getByRole('button', { name: '音声で回答', exact: true }).waitFor() }
  async function start() { await page.getByRole('button', { name: '音声で回答', exact: true }).click(); await page.getByRole('button', { name: '録音を停止', exact: true }).waitFor() }
  async function finish(index = 0, text = '音声') { const count = await page.evaluate(() => window.callbacks.length); await page.getByRole('button', { name: '録音を停止', exact: true }).click(); await page.evaluate(({ index, text }) => window.recorders[index].finish(text), { index, text }); await page.waitForFunction(count => window.callbacks.length > count, count) }
  const draft = () => page.getByRole('textbox', { name: 'あなたの説明' })

  await reset(); await start();
  await page.getByRole('button', { name: '録音を取り消す' }).click(); await start();
  await page.evaluate(() => window.recorders[0].finish('古い音声'));
  assert.equal(await page.evaluate(() => window.streams[1].getTracks()[0].stopped), false);
  assert.equal(await page.evaluate(() => window.calls.length), 0);
  await finish(1, '新しい音声');
  assert.equal(await page.evaluate(() => window.calls[0].text), '新しい音声');
  console.log('PASS 取消直後の再録音と遅延onstopの隔離');

  await page.evaluate(() => window.callbacks[0]('認識結果'));
  assert.equal(await draft().inputValue(), '元の下書き');
  assert.equal(await page.evaluate(() => window.sent.length), 0);
  await page.getByRole('button', { name: '回答の後ろに追加' }).click();
  assert.equal(await draft().inputValue(), '元の下書き 認識結果');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  assert.deepEqual(await page.evaluate(() => window.sent), ['元の下書き 認識結果']);
  await start(); await finish(2);
  await page.evaluate(() => window.callbacks[1]('置換結果'));
  await page.getByRole('button', { name: '回答欄に入れる' }).click();
  assert.equal(await draft().inputValue(), '置換結果');
  console.log('PASS 下書き保持・追加・置換・明示送信');

  await start(); await finish(3); await start();
  await page.evaluate(() => window.callbacks[2]('古い文字起こし'));
  assert.equal(await page.getByRole('region', { name: '文字起こし結果' }).count(), 0);
  await page.evaluate(() => window.recorders[4].onerror());
  await start();
  assert.equal(await page.evaluate(() => window.streams[4].getTracks()[0].stopped), true);
  await page.evaluate(() => window.recorders[4].finish('エラー後の旧音声'));
  assert.equal(await page.evaluate(() => window.streams[5].getTracks()[0].stopped), false);
  await page.evaluate(() => window.hide());
  await page.waitForFunction(() => window.streams[5].getTracks()[0].stopped);
  console.log('PASS 古い文字起こし・録音エラー・unmountの後処理');

  await reset(); await page.evaluate(() => window.deferMic = true);
  await page.getByRole('button', { name: '音声で回答', exact: true }).evaluate(button => { button.click(); button.click() });
  assert.equal(await page.evaluate(() => window.requests.length), 1);
  await page.getByRole('button', { name: '録音を取り消す' }).click();
  await page.getByRole('button', { name: '音声で回答', exact: true }).click();
  await page.evaluate(() => window.requests[0]());
  assert.equal(await page.evaluate(() => window.streams[0].getTracks()[0].stopped), true);
  assert.equal(await page.evaluate(() => window.recorders.length), 0);
  await page.evaluate(() => window.requests[1]());
  await page.getByRole('button', { name: '録音を停止', exact: true }).waitFor();
  console.log('PASS 同一tick連打・許可待ち取消・再許可');

  await reset(); await page.evaluate(() => window.deferMic = true);
  await page.getByRole('button', { name: '音声で回答', exact: true }).click();
  await page.evaluate(() => { window.hide(); });
  await page.waitForFunction(() => !document.querySelector('textarea'));
  await page.evaluate(() => window.requests[0]());
  assert.equal(await page.evaluate(() => window.streams[0].getTracks()[0].stopped), true);
  assert.equal(await page.evaluate(() => window.recorders.length), 0);
  console.log('PASS 許可待ちunmount');

  await reset(); await page.evaluate(() => window.rejectMic = true);
  await page.getByRole('button', { name: '音声で回答', exact: true }).click();
  await page.getByText('マイクを利用できません。許可を確認するか、文字で入力してください。').waitFor();
  assert.equal(await draft().inputValue(), '元の下書き');
  await page.evaluate(() => { window.rejectMic = false; window.failStart = true });
  await page.getByRole('button', { name: '音声で回答', exact: true }).click();
  await page.getByText('この録音形式には対応していません。文字で入力してください。').waitFor();
  assert.equal(await page.evaluate(() => window.streams[1].getTracks()[0].stopped), true);
  console.log('PASS マイク拒否・start失敗');

  await reset(); await page.clock.install(); await start();
  await page.clock.fastForward(30_000);
  assert.equal(await page.evaluate(() => window.recorders[0].state), 'inactive');
  assert.equal(await page.evaluate(() => window.streams[0].getTracks()[0].stopped), true);
  assert.equal(await page.evaluate(() => window.calls.length), 0);
  await page.evaluate(() => window.recorders[0].finish('最後のチャンク'));
  await page.waitForFunction(() => window.calls.length === 1);
  assert.equal(await page.evaluate(() => window.calls[0].text), '最後のチャンク');
  console.log('PASS 30秒自動停止と最終チャンク待機');
  assert.deepEqual(await page.evaluate(async () => ({local: localStorage.length, session: sessionStorage.length, db: (await indexedDB.databases()).length, cookies: document.cookie})), {local:0, session:0, db:0, cookies:''});
  await reset(); await start();
  await page.getByRole('button', { name: '録音を停止', exact: true }).click();
  await page.evaluate(() => window.recorders[0].finish(''));
  await page.getByText('音声を録音できませんでした。もう一度お試しください。').waitFor();
  assert.equal(await page.evaluate(() => window.calls.length), 0);
  await start(); await page.getByRole('button', { name: '録音を停止', exact: true }).click();
  await page.evaluate(() => window.recorders[1].finish('a'.repeat(5 * 1024 * 1024 + 1)));
  await page.getByText('録音が5 MiBを超えました。短く録音してください。').waitFor();
  assert.equal(await page.evaluate(() => window.calls.length), 0);
  await start(); await page.getByRole('button', { name: '録音を停止', exact: true }).click();
  await page.evaluate(() => window.recorders[2].finish('a'.repeat(5 * 1024 * 1024)));
  await page.waitForFunction(() => window.calls.length === 1);
  console.log('PASS 空音声・5 MiBちょうど・1バイト超過');
  const synthesis = [];
  let holdSynthesis = false;
  let synthesisStatus = 200;
  await page.route(/\/api\/speech\/synthesize$/, async route => {
    synthesis.push(route);
    if (!holdSynthesis) await route.fulfill({ status: synthesisStatus, contentType: 'audio/mpeg', body: 'mock-mp3' });
  });
  await reset(); await page.evaluate(() => window.newQuestion('同じ質問？'));
  await page.waitForFunction(() => window.players[0]?.plays === 1);
  const firstURL = await page.evaluate(() => window.players[0].src);
  await page.evaluate(() => window.speech.stop());
  assert.equal(await page.evaluate(() => window.players[0].paused), true);
  await page.evaluate(() => { window.speech.play(); window.speech.play() });
  await page.waitForFunction(() => window.players[1]?.plays === 1);
  assert.equal(synthesis.length, 2, '停止後の再生は解放済み音声を取得し直す');
  assert.ok(await page.evaluate(url => window.revoked.includes(url), firstURL));
  assert.equal(await page.evaluate(() => window.players[0].src), '');
  await page.evaluate(() => window.newQuestion('同じ質問？'));
  await page.waitForFunction(() => window.players[2]?.plays === 1);
  assert.equal(synthesis.length, 3, '同文でも新しいターンは音声を生成する');
  assert.ok(await page.evaluate(url => window.revoked.includes(url), firstURL));
  await page.evaluate(() => window.deferMic = true);
  await page.getByRole('button', { name: '音声で回答', exact: true }).click();
  assert.equal(await page.evaluate(() => window.players[2].paused), true);
  await page.getByRole('button', { name: '録音を取り消す' }).click();
  await page.evaluate(() => window.hide());
  await page.waitForFunction(() => window.players[2].src === '');
  console.log('PASS TTS停止・音声解放・再取得・同文の別ターン・録音許可待ち・unmount解放');

  holdSynthesis = true;
  await reset(); const beforeLate = synthesis.length;
  await page.evaluate(() => window.newQuestion('古い質問？'));
  await page.waitForFunction(() => window.speech.phase === 'loading');
  for (let retry = 0; synthesis.length === beforeLate && retry < 500; retry++) await new Promise(resolve => setTimeout(resolve, 10));
  await start();
  await synthesis.at(-1).fulfill({ contentType: 'audio/mpeg', body: 'late-mp3' }).catch(() => {});
  assert.equal(await page.evaluate(() => window.players.length), 0, '録音開始後の旧応答は再生しない');
  await page.getByRole('button', { name: '録音を取り消す' }).click();
  holdSynthesis = false;
  await page.evaluate(() => window.speech.play());
  await page.waitForFunction(() => window.players[0]?.plays === 1);
  assert.equal(await draft().inputValue(), '元の下書き');
  assert.equal(await page.evaluate(() => window.sent.length), 0);
  console.log('PASS TTS遅延応答の無視・録音取消後の手動再生・下書き維持');

  await reset(); await page.evaluate(() => { window.rejectPlay = true; window.newQuestion('再生できる？') });
  await page.getByText('自動再生できませんでした。再生ボタンを押してください。').waitFor();
  await page.evaluate(() => { window.rejectPlay = false; window.speech.play() });
  await page.waitForFunction(() => !window.players[0].paused);
  for (const status of [400, 401, 404, 413, 415, 429, 500, 502, 503, 504]) {
    synthesisStatus = status;
    await page.evaluate(text => window.newQuestion(text), `障害${status}？`);
    await page.waitForFunction(() => window.speech.phase === 'idle' && Boolean(window.speech.error));
    assert.equal(await draft().inputValue(), '元の下書き');
    assert.equal(await page.evaluate(() => window.sent.length), 0);
  }
  await page.evaluate(() => window.speech.stop());
  await page.waitForFunction(() => window.speech.error === '' && window.speech.phase === 'idle');
  synthesisStatus = 200;
  await reset();
  await page.evaluate(() => { window.deferPlay = true; window.newQuestion('遅れて再生する質問？') });
  await page.waitForFunction(() => window.pendingPlays.length === 1);
  await page.evaluate(() => {
    window.oldCallbacks = [window.players[0].onplay, window.players[0].onpause, window.players[0].onended];
    window.speech.stop();
  });
  assert.equal(await page.evaluate(() => window.players[0].src), '');
  await page.evaluate(() => { window.deferPlay = false; window.speech.play(); window.speech.play() });
  await page.waitForFunction(() => window.players[1]?.plays === 1 && window.speech.phase === 'playing');
  await page.evaluate(() => { window.pendingPlays[0](); window.oldCallbacks.forEach(callback => callback?.()) });
  await page.waitForFunction(() => window.players[0].paused);
  assert.equal(await page.evaluate(() => window.players[1].paused), false);
  assert.equal(await page.evaluate(() => window.speech.phase), 'playing');
  await page.evaluate(() => window.hide());
  await page.waitForFunction(() => window.players[1].src === '' && window.speech.phase === 'idle' && window.speech.error === '');
  await page.evaluate(() => window.show());
  await page.getByRole('button', { name: '音声で回答', exact: true }).waitFor();
  await page.evaluate(() => window.speech.play());
  await page.waitForFunction(() => window.players[2]?.plays === 1);
  await page.evaluate(() => window.unmount());
  await page.waitForFunction(() => window.players[2].src === '');
  assert.equal(await page.evaluate(() => window.revoked.length), 3);
  console.log('PASS TTS停止のエラー解除・遅延playと旧イベントの隔離・非表示とunmountの音声解放');
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  console.log('PASS 自動再生拒否からの手動再生・TTS各障害でも文字の学習と下書きを維持');
  await page.unroute(/\/api\/speech\/synthesize$/);
  const apiCalls = [];
  let speechRoute;
  await page.route(/\/api\/(start|chat|speech\/(transcribe|synthesize))$/, async route => {
    const request = route.request(); apiCalls.push(request.url().split('/api/')[1]);
    if (request.url().endsWith('/start')) return route.fulfill({ json: { sessionId: '11111111-1111-4111-8111-111111111111', reply: 'どんな教材ですか？' } });
    if (request.url().endsWith('/speech/transcribe')) { speechRoute = route; return }
    if (request.url().endsWith('/speech/synthesize')) return route.fulfill({ contentType: 'audio/mpeg', body: 'mock-mp3' });
    return route.fulfill({ json: { reply: '理由は何ですか？' } });
  });
  await page.goto(server.resolvedUrls.local[0]);
  await page.getByRole('button', { name: '学習をはじめる' }).click();
  await page.getByRole('textbox', { name: '本文', exact: true }).fill('自作のテスト教材');
  await page.getByRole('button', { name: '説明をはじめる' }).click();
  await draft().fill('通信中も残す下書き');
  await start(); await page.getByRole('button', { name: '録音を停止', exact: true }).click();
  await page.evaluate(() => window.recorders[0].finish());
  await page.getByRole('button', { name: '文字起こしを取り消す' }).waitFor();
  await page.getByRole('button', { name: '文字起こしを取り消す' }).click();
  assert.equal(await draft().inputValue(), '通信中も残す下書き');
  if (speechRoute) await speechRoute.fulfill({ json: { text: '取り消した応答' } }).catch(() => {});
  await start(); await page.getByRole('button', { name: '録音を停止', exact: true }).click();
  speechRoute = undefined;
  await page.evaluate(() => window.recorders[1].finish());
  await page.getByRole('button', { name: '文字起こしを取り消す' }).waitFor();
  for (let retry = 0; !speechRoute && retry < 500; retry++) await new Promise(resolve => setTimeout(resolve, 10));
  assert.ok(speechRoute, '文字起こしリクエストが届く');
  await speechRoute.fulfill({ json: { text: '新しい認識結果' } });
  await page.getByRole('region', { name: '文字起こし結果' }).waitFor();
  assert.equal(await draft().inputValue(), '通信中も残す下書き');
  assert.equal(apiCalls.filter(path => path === 'chat').length, 0);
  await page.getByRole('button', { name: '回答欄に入れる' }).click();
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await page.getByText('ゼロ：理由は何ですか？', { exact: true }).waitFor();
  assert.equal(apiCalls.filter(path => path === 'chat').length, 1);
  console.log('PASS App実通信経路の取消・再試行・明示送信（APIモック）');
  await page.unrouteAll({ behavior: 'wait' });
  const phase2Calls = [];
  const expires = Math.floor(await page.evaluate(() => Date.now()) / 1000) + 3600;
  const sessionId = '11111111-1111-4111-8111-111111111111';
  const initialProof = `s1.test.${expires}.${'a'.repeat(43)}`;
  const updatedProof = `s1.test.${expires}.${'b'.repeat(43)}`;
  const saved = { sessionId, summary: { understood: [], review: [] }, saved: true, viewKey: '7KQM-3XWD-9HTR', savedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 30 * 86400000).toISOString() };
  let summaryAttempts = 0;
  await page.route(/\/api\/(start|chat|summary|history\/open|speech\/synthesize)$/, async route => {
    const path = route.request().url().split('/api/')[1];
    const body = route.request().postDataJSON();
    phase2Calls.push({ path, body });
    if (path === 'speech/synthesize') return route.fulfill({ status: 503, json: { error: { code: 'SYNTHESIS_UNAVAILABLE' } } });
    if (path === 'start') return route.fulfill({ json: { sessionId, reply: 'どんな教材ですか？', sessionProof: initialProof, sessionExpiresAt: new Date(expires * 1000).toISOString() } });
    if (path === 'chat') return route.fulfill({ json: { reply: 'どんな教材ですか？', sessionProof: updatedProof } });
    if (path === 'summary') {
      if (++summaryAttempts === 1) return route.fulfill({ status: 503, json: { error: { code: 'STORAGE_UNAVAILABLE' } } });
      return route.fulfill({ json: saved });
    }
    return route.fulfill({ json: { ...saved, learningContent: '署名つき教材', history: [{ role: 'model', content: 'どんな教材ですか？' }, { role: 'user', content: '署名つきの回答です' }, { role: 'model', content: 'どんな教材ですか？' }] } });
  });
  await page.goto(server.resolvedUrls.local[0]);
  await page.getByRole('button', { name: '学習をはじめる' }).click();
  await page.getByRole('textbox', { name: '本文', exact: true }).fill('署名つき教材');
  await page.reload();
  assert.equal(await page.getByRole('textbox', { name: '本文', exact: true }).inputValue(), '署名つき教材');
  await page.getByRole('button', { name: '説明をはじめる' }).click();
  await page.locator('[data-screen="explain"]').waitFor();
  await draft().fill('署名つきの回答です');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await page.locator('[data-screen="chat"]').waitFor();
  await page.getByText('ただいま読み上げを利用できません。文字で質問を確認してください。').waitFor();
  assert.equal(phase2Calls.find(call => call.path === 'chat').body.sessionProof, initialProof);
  const storageKey = 'ai-ni-oshieru.learning.v1';
  let state = await page.evaluate(key => JSON.parse(localStorage.getItem(key)), storageKey);
  assert.equal(state.session.sessionProof, updatedProof);
  assert.equal(state.session.history.length, 3);
  assert.equal(phase2Calls.filter(call => call.path === 'speech/synthesize').length, 2);
  await page.getByRole('textbox', { name: 'ゼロへの回答' }).fill('復元する下書き');
  await page.reload();
  assert.equal(await page.getByRole('textbox', { name: 'ゼロへの回答' }).inputValue(), '復元する下書き');
  await page.getByRole('textbox', { name: 'ゼロへの回答' }).fill('');
  await page.getByRole('button', { name: '理解を確かめる', exact: true }).click();
  await page.getByRole('checkbox').first().check();
  await page.getByRole('button', { name: '振り返る', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: '混み合っています' }).waitFor();
  state = await page.evaluate(key => JSON.parse(localStorage.getItem(key)), storageKey);
  assert.equal(state.frozen, true);
  await page.reload();
  assert.equal(await page.getByRole('button', { name: '対話を続ける' }).isDisabled(), true);
  assert.equal(await page.getByRole('checkbox').first().isChecked(), true);
  await page.getByRole('button', { name: '振り返る', exact: true }).click();
  await page.getByRole('region', { name: '保存完了' }).waitFor();
  const summaries = phase2Calls.filter(call => call.path === 'summary');
  assert.equal(summaries.length, 2);
  assert.deepEqual(summaries[0].body, summaries[1].body);
  assert.equal(summaries[1].body.sessionProof, updatedProof);
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  assert.equal(await page.evaluate(() => sessionStorage.length), 0);
  assert.equal(page.url().includes(saved.viewKey), false);
  await page.getByRole('button', { name: 'ホームへ', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: '戻る', exact: true }).click();
  await page.getByRole('button', { name: '閲覧キーで開く', exact: true }).click();
  await page.getByRole('textbox', { name: '閲覧キー', exact: true }).fill(saved.viewKey);
  await page.getByRole('button', { name: '記録を開く', exact: true }).click();
  await page.getByRole('heading', { name: '保存した学習記録' }).waitFor();
  assert.deepEqual(phase2Calls.at(-1), { path: 'history/open', body: { viewKey: saved.viewKey } });
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  console.log('PASS Phase2署名更新・同文TTS・下書き復元・保存不明freeze復元・同一再送・再閲覧・キー非永続化');

  await page.evaluate(({ storageKey, state }) => {
    const expiresAt = new Date(Math.floor(Date.now() / 1000) * 1000 + 20000).toISOString();
    state.expiresAt = expiresAt;
    state.session.sessionProof = `s1.test.${Date.parse(expiresAt) / 1000}.${'c'.repeat(43)}`;
    state.frozen = false;
    localStorage.setItem(storageKey, JSON.stringify(state));
  }, { storageKey, state });
  await page.goto(server.resolvedUrls.local[0]);
  await start();
  await page.clock.fastForward(20000);
  await page.getByText('学習期限を過ぎたため一時保存を削除しました。対話はこの画面で確認できますが、送信・保存・再試行はできません。').waitFor();
  assert.equal(await page.getByRole('button', { name: '音声で回答', exact: true }).isDisabled(), true);
  assert.equal(await page.evaluate(() => window.streams[0].getTracks()[0].stopped), true);
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  await page.getByRole('button', { name: '理解を確かめる', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: '振り返る', exact: true }).isDisabled(), true);
  assert.equal(phase2Calls.filter(call => call.path === 'summary').length, 2);
  console.log('PASS 期限切れで進行中録音を解放し送信・保存を停止');
  await page.addInitScript(() => {
    Storage.prototype.setItem = () => { throw new DOMException('blocked', 'QuotaExceededError') };
  });
  await page.goto(server.resolvedUrls.local[0]);
  await page.getByRole('button', { name: '学習をはじめる' }).click();
  await page.getByRole('textbox', { name: '本文', exact: true }).fill('一時保存を拒否する教材');
  await page.getByText('教材の下書きを一時保存できません。画面を閉じると失われます。').waitFor();
  await page.getByRole('button', { name: '説明をはじめる' }).click();
  await page.getByText('このブラウザでは一時保存できません。画面を閉じると学習を再開できません。').waitFor();
  await draft().fill('保存拒否でも続けられる回答');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await page.locator('[data-screen="chat"]').waitFor();
  assert.equal(phase2Calls.filter(call => call.path === 'chat').at(-1).body.sessionProof, initialProof);
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  console.log('PASS Storage拒否時の警告と署名付き対話の継続');
  assert.deepEqual(errors, []);
  console.log('PASS 音声・保存結果・閲覧キーの永続化なし・ブラウザエラーなし');
} finally {
  await browser?.close()
  await server.close()
}
