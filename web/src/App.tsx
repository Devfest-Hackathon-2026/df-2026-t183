import { useEffect, useRef, useState } from 'react'
import { Conversation, SelfCheck, checkItems } from './features/learning/Conversation'
import { Home } from './features/learning/Home'
import { Material } from './features/learning/Material'
import type { MaterialDraft } from './features/learning/Material'
import { Reflection } from './features/learning/Reflection'
import { useSpeechPlayback } from './features/learning/useSpeechPlayback'
import { ConfirmDialog, TopBar } from './features/learning/ui'
import type { Confirmation } from './features/learning/ui'
import { normalizeText, TURN_LIMIT } from './features/learning/limits'
import { loadLearning, removeLearning, saveLearning, loadMaterial, saveMaterial, removeMaterial } from './lib/session'
import { History } from './features/learning/History'
import { Alert, Note } from './features/learning/ui'
import { continueLearning, startLearning, summarizeLearning, transcribeSpeech } from './lib/api/learning'
import type { LearningResult, Turn } from './lib/api/learning'

type Screen = 'home' | 'material' | 'explain' | 'chat' | 'check' | 'reflection' | 'history'

const emptyMaterial: MaterialDraft = { mode: 'direct', text: '' }
const screens: Record<Exclude<Screen, 'home'>, { title: string; step: number }> = {
  history: { title: '保存した記録', step: 5 },
  material: { title: '教材を入力', step: 1 },
  explain: { title: '説明する', step: 2 },
  chat: { title: '質問に答える', step: 3 },
  check: { title: '理解を確かめる', step: 4 },
  reflection: { title: '振り返り', step: 5 },
}

export function App() {
  const [restored] = useState(() => { try { return loadLearning(window.localStorage) } catch { return undefined } })
  const [initialDraft] = useState(() => { try { return loadMaterial(window.localStorage) } catch { return undefined } })
  const draftDeadline = useRef(initialDraft?.expiresAt ?? 0)
  const [expired, setExpired] = useState(false)
  const [expiring, setExpiring] = useState(false)
  const [screen, setScreen] = useState<Screen>(restored ? restored.frozen ? 'check' : 'chat' : initialDraft ? 'material' : 'home')
  const [material, setMaterial] = useState(restored ? { mode: 'direct' as const, text: restored.session.learningContent } : initialDraft ? { mode: 'direct' as const, text: initialDraft.text } : emptyMaterial)
  const [history, setHistory] = useState<Turn[]>(restored?.session.history ?? [])
  const [voiceTurn, setVoiceTurn] = useState(0)
  const [sessionId, setSessionId] = useState(restored?.session.sessionId ?? '')
  const [sessionProof, setSessionProof] = useState(restored?.session.sessionProof ?? '')
  const [expiresAt, setExpiresAt] = useState(restored?.expiresAt ?? '')
  const [frozen, setFrozen] = useState(restored?.frozen ?? false)
  const freeze = useRef(restored?.frozen ?? false)
  const [storageError, setStorageError] = useState('')
  const [answer, setAnswer] = useState(restored?.draft ?? '')
  const [summary, setSummary] = useState<LearningResult>()
  const [checked, setChecked] = useState<boolean[]>(() => restored?.checked ?? checkItems.map(() => false))
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [pendingLabel, setPendingLabel] = useState('')
  const [confirmation, setConfirmation] = useState<Confirmation & { resolve: (ok: boolean) => void }>()
  const request = useRef<AbortController | null>(null)
  const asking = useRef(false)
  const generation = useRef(0)
  const frame = useRef<HTMLDivElement>(null)
  const initial = useRef(true)
  const hasWork = Boolean(material.text || history.length || answer || pending)
  const canResume = !expired && !frozen && history.length < TURN_LIMIT * 2 + 1

  const latestQuestion = history.at(-1)?.role === 'model' ? history[history.length - 1].content : ''
  const speech = useSpeechPlayback(latestQuestion, voiceTurn, !expired && (screen === 'explain' || screen === 'chat'))

  function removeStored() {
    try {
      const sessionRemoved = removeLearning(window.localStorage)
      const materialRemoved = removeMaterial(window.localStorage)
      if (!sessionRemoved || !materialRemoved) throw new Error()
    }
    catch { setStorageError('端末の一時保存を削除できません。ブラウザのサイトデータを削除してください。') }
  }
  useEffect(() => {
    if (!sessionId || !sessionProof || summary || expired) return
    try {
      if (!saveLearning(window.localStorage, { version: 1, session: { sessionId, sessionProof, learningContent: material.text, history }, expiresAt, draft: answer, checked, frozen })) throw new Error()
    } catch { setStorageError('このブラウザでは一時保存できません。画面を閉じると学習を再開できません。') }
  }, [sessionId, sessionProof, material.text, history, expiresAt, answer, checked, frozen, summary, expired])

  useEffect(() => {
    if (sessionId || screen !== 'material') return
    if (!material.text) {
      try { removeMaterial(window.localStorage) } catch { /* No persisted draft is required. */ }
      return
    }
    if (!draftDeadline.current) draftDeadline.current = Date.now() + 7200000
    if (draftDeadline.current <= Date.now()) return
    try {
      if (!saveMaterial(window.localStorage, material.text, draftDeadline.current)) throw new Error()
    } catch { setStorageError('教材の下書きを一時保存できません。画面を閉じると失われます。') }
  }, [material.text, screen, sessionId])

  useEffect(() => {
    if (sessionId || screen !== 'material' || !draftDeadline.current) return
    const timer = window.setTimeout(() => {
      try { removeMaterial(window.localStorage) } catch { /* Storage may be unavailable. */ }
      setStorageError('教材下書きの一時保存期限を過ぎました。画面上の本文はそのまま使えますが、再読み込みすると失われます。')
    }, Math.max(0, draftDeadline.current - Date.now()))
    return () => window.clearTimeout(timer)
  }, [screen, material.text, sessionId])

  useEffect(() => {
    if (!expiresAt || summary) return
    const remaining = Date.parse(expiresAt) - Date.now()
    const warning = window.setTimeout(() => setExpiring(true), Math.max(0, remaining - 900000))
    const timer = window.setTimeout(() => {
      cancelRequest()
      removeStored()
      setExpired(true)
      setStorageError('学習期限を過ぎたため一時保存を削除しました。対話はこの画面で確認できますが、送信・保存・再試行はできません。')
    }, Math.max(0, remaining))
    return () => { window.clearTimeout(timer); window.clearTimeout(warning) }
  }, [expiresAt, summary])

  useEffect(() => {
    if (initial.current) {
      initial.current = false
      return
    }
    if (screen !== 'chat') window.scrollTo(0, 0)
    frame.current?.querySelector('h1')?.focus({ preventScroll: true })
  }, [screen])

  useEffect(() => {
    if (!hasWork) return
    function warn(event: BeforeUnloadEvent) {
      event.preventDefault()
      event.returnValue = true
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [hasWork])

  useEffect(() => () => request.current?.abort(), [])

  function ask(question: Confirmation) {
    if (asking.current) return Promise.resolve(false)
    asking.current = true
    speech.stop()
    return new Promise<boolean>((resolve) => setConfirmation({ ...question, resolve }))
  }

  function cancelRequest() {
    request.current?.abort()
    request.current = null
    setPending(false)
    setPendingLabel('')
  }

  async function run<T>(operation: (signal: AbortSignal) => Promise<T>, onSuccess: (data: T) => void, label = 'ゼロが質問を考えています…') {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setPendingLabel(label)
    setError('')
    try {
      const data = await operation(controller.signal)
      if (!controller.signal.aborted) onSuccess(data)
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(cause instanceof Error ? cause.message : '通信に失敗しました。もう一度お試しください。')
      }
    } finally {
      if (request.current === controller) {
        request.current = null
        setPending(false)
        setPendingLabel('')
      }
    }
  }

  function clearSession() {
    draftDeadline.current = 0
    setExpired(false)
    setExpiring(false)
    setStorageError('')
    generation.current += 1
    cancelRequest()
    removeStored()
    freeze.current = false
    setFrozen(false)
    setSessionProof('')
    setExpiresAt('')
    setSessionId('')
    setHistory([])
    setAnswer('')
    setChecked(checkItems.map(() => false))
    setSummary(undefined)
    setError('')
  }

  async function home() {
    if (hasWork && !await ask({
      title: 'ホームに戻りますか？',
      body: screen === 'reflection' ? summary?.saved ? '閲覧キーを控えましたか？キーはこのブラウザに保存されません。' : '振り返りは保存されていません。' : frozen ? '保存された可能性があります。戻ると再試行できなくなり、閲覧キーを取得できません。' : '端末に一時保存された入力と対話を破棄します。',
      confirm: '戻る',
    })) return
    clearSession()
    setMaterial(emptyMaterial)
    setScreen('home')
  }

  async function back() {
    if (screen === 'check') {
      if (freeze.current) { await home(); return }
      cancelRequest()
      setError('')
      setScreen('chat')
      return
    }
    if (screen === 'chat' || screen === 'explain') {
      if (!await ask({ title: '教材の入力に戻りますか？', body: 'ここまでの対話は消えます。', confirm: '戻る' })) return
      clearSession()
      setScreen('material')
      return
    }
    await home()
  }

  function begin(learningContent: string) {
    void run((signal) => startLearning(learningContent, signal), ({ sessionId, reply, sessionProof, sessionExpiresAt }) => {
      try { removeMaterial(window.localStorage) } catch { /* Storage warning is shown by persistence. */ }
      setSessionProof(sessionProof ?? '')
      setExpiresAt(sessionExpiresAt ?? '')
      setMaterial((current) => ({ ...current, text: learningContent }))
      setSessionId(sessionId)
      setHistory([{ role: 'model', content: reply }])
      setVoiceTurn((value) => value + 1)
      setScreen('explain')
    })
  }

  function send(message: string) {
    if (!canResume || freeze.current) return
    void run((signal) => continueLearning(learningRequest(), message, signal), ({ reply, sessionProof }) => {
      setHistory([...history, { role: 'user', content: message }, { role: 'model', content: reply }])
      setVoiceTurn((value) => value + 1)
      setSessionProof(sessionProof ?? '')
      setAnswer('')
      setScreen('chat')
    })
  }

  function transcribe(audio: Blob, contentType: string, onSuccess: (text: string) => void) {
    void run((signal) => transcribeSpeech(audio, contentType, signal), onSuccess, '音声を文字にしています…')
  }

  function learningRequest() {
    return { sessionId, learningContent: material.text, history, ...(sessionProof ? { sessionProof } : {}) }
  }

  async function finish() {
    if (expired || request.current || pending || history.length < 3) return
    const expectedGeneration = generation.current
    if (!freeze.current && normalizeText(answer) && !await ask({
      title: '未送信の回答があります',
      body: '送信せずに振り返りますか？',
      confirm: '振り返る',
    })) return
    if (request.current || expectedGeneration !== generation.current || Date.parse(expiresAt) <= Date.now()) return
    if (sessionProof) {
      freeze.current = true
      setFrozen(true)
      try {
        if (!saveLearning(window.localStorage, { version: 1, session: { sessionId, sessionProof, learningContent: material.text, history }, expiresAt, draft: answer, checked, frozen: true })) throw new Error()
      } catch { setStorageError('一時保存できません。この画面を閉じると保存の再試行ができません。') }
    }
    void run((signal) => summarizeLearning(learningRequest(), signal), (result) => {
      removeStored()
      setSummary(result)
      setScreen('reflection')
    })
  }

  return <div className="app" data-screen={screen} ref={frame}>
    {storageError && <div className="session-notice"><Alert message={storageError} /></div>}
    {expiresAt && !summary && <div className="session-notice"><Note>学習期限: {new Date(expiresAt).toLocaleString()}。{frozen ? '保存結果が未確認です。失敗した場合は同じ内容で「振り返る」を押して再試行してください。対話は変更できません。' : expiring ? '残り15分以内です。期限までに振り返りと保存を完了してください。' : '学習中の内容はこの端末に一時保存されます。'}</Note></div>}
    {screen !== 'home' && <TopBar title={screens[screen].title} step={screens[screen].step} onBack={() => void back()} />}
    {screen === 'home' && <Home onStart={() => setScreen('material')} onOpen={() => setScreen('history')} />}
    {screen === 'history' && <History />}
    {screen === 'material' && <Material draft={material} onChange={(next) => {
      setMaterial(next)
      setError('')
    }} onStart={begin} error={error} pending={pending} />}
    {(screen === 'explain' || screen === 'chat') && <Conversation
      history={history} material={{ name: material.file?.name, text: material.text }}
      speech={speech}
      draft={answer} onDraft={setAnswer} onSend={send} onCheck={() => {
        setError('')
        setScreen('check')
      }} onTranscribe={transcribe} onCancelRequest={cancelRequest}
      pending={pending} pendingLabel={pendingLabel} disabled={expired} error={error} />}
    {screen === 'check' && <SelfCheck checked={checked} onCheck={(index, selected) => {
      setChecked((values) => values.map((value, position) => position === index ? selected : value))
    }} onResume={() => void back()} onFinish={() => void finish()} canResume={canResume} disabled={expired} pending={pending} error={error} />}
    {screen === 'reflection' && summary && <Reflection result={summary.summary} saved={summary.saved ? summary : undefined} history={history} checked={checked} onHome={() => void home()} />}
    {confirmation && <ConfirmDialog title={confirmation.title} body={confirmation.body} confirm={confirmation.confirm}
      onClose={(ok) => {
        asking.current = false
        setConfirmation(undefined)
        confirmation.resolve(ok)
      }} />}
  </div>
}
