import { useEffect, useRef } from 'react'
import type { FormEvent } from 'react'
import { useRecording } from './useRecording'
import type { Turn } from '../../lib/api/learning'
import { ANSWER_LIMIT, normalizeText, textLength, TURN_LIMIT } from './limits'
import { Alert, Icon, Message, Note, Spinner, Thinking } from './ui'

const format = (number: number) => number.toLocaleString('ja-JP')
const motion = (): ScrollBehavior => window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'

export function Conversation({
  history, material, draft, onDraft, onSend, onCheck, onTranscribe, onCancelRequest, speech, pending, pendingLabel, error, disabled = false,
}: {
  history: Turn[]
  material: { name?: string; text: string }
  draft: string
  onDraft: (value: string) => void
  onSend: (value: string) => void
  onCheck: () => void
  onTranscribe: (audio: Blob, type: string, done: (text: string) => void) => void
  onCancelRequest: () => void
  speech: { phase: 'idle' | 'loading' | 'ready' | 'playing'; error: string; play: () => void; stop: () => void }
  pendingLabel: string
  pending: boolean
  disabled?: boolean
  error: string
}) {
  const voice = useRecording(onTranscribe)
  const busy = voice.phase !== 'idle'
  const transcribing = pending && pendingLabel === '音声を文字にしています…'
  const list = useRef<HTMLDivElement>(null)
  const round = (history.length - 1) / 2
  const first = round === 0
  const reachedLimit = round >= TURN_LIMIT
  const overLimit = textLength(draft) > ANSWER_LIMIT
  const canSend = !disabled && !pending && !busy && !reachedLimit && !overLimit && Boolean(normalizeText(draft))

  useEffect(() => {
    if (disabled) {
      voice.cancel()
      speech.stop()
    }
  }, [disabled])

  useEffect(() => {
    if (history.length < 2) return
    const replies = list.current?.querySelectorAll('.message--model')
    replies?.[replies.length - 1]?.scrollIntoView({ block: 'start', behavior: motion() })
  }, [history.length])

  useEffect(() => {
    if (pending) window.scrollTo({ top: document.documentElement.scrollHeight, behavior: motion() })
  }, [pending])

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (canSend) {
      speech.stop()
      onSend(normalizeText(draft))
    }
  }

  return <>
    <main className="flow">
      {first && <p className="chip">
        <Icon name="description" size={18} />
        <span>{material.name ?? '教材'} · {format(textLength(material.text))}字</span>
      </p>}
      <div className="message-list" role="log" aria-label="ゼロとの対話" ref={list}>
        {history.map((turn, index) => <Message key={index} {...turn}
          speech={index === history.length - 1 && turn.role === 'model' ? {
            phase: speech.phase, disabled: disabled || busy || pending, onPlay: speech.play, onStop: speech.stop,
          } : undefined} />)}
        {pending && !transcribing && <Thinking />}
      </div>
      {speech.error && <p className="speech-error" role="status">{speech.error}</p>}
      {first && <p className="hint"><Icon name="lightbulb" size={20} /><span>何の話？・言葉の意味は？・なぜそうなる？</span></p>}
    </main>
    <footer className={reachedLimit ? 'bottom bottom--actions' : 'bottom bottom--chat'}>
      {error && <Alert message={error} />}
      {voice.error && <Alert message={voice.error} />}
      {busy && <div className="voice-status" role="status">
        <span>{voice.phase === 'requesting' ? 'マイクの許可を待っています…' : voice.phase === 'stopping' ? '録音を処理しています…' : `録音中 ${voice.seconds} / 30秒`}</span>
        <button className="button text" type="button" onClick={voice.cancel}>録音を取り消す</button>
      </div>}
      {transcribing && <div className="voice-status" role="status"><span><Spinner />{pendingLabel}</span>
        <button className="button text" type="button" onClick={() => { voice.cancel(); onCancelRequest() }}>文字起こしを取り消す</button>
      </div>}
      {reachedLimit ? <>
        <Note>{`${TURN_LIMIT}往復に達しました`}</Note>
        <button className="button filled" type="button" disabled={pending || busy} onClick={onCheck}>理解を確かめる</button>
      </> : <>
        {!first && <button className="chip-button" type="button" disabled={pending || busy} onClick={onCheck}>
          <Icon name="checklist" size={18} />理解を確かめる
        </button>}
        <form className="composer" onSubmit={submit}>
          <textarea id="answer" rows={1} aria-label={first ? 'あなたの説明' : 'ゼロへの回答'}
            aria-invalid={overLimit} aria-describedby={overLimit ? 'answer-error' : undefined}
            placeholder={first ? '自分の言葉で説明する' : '質問に答える'} value={draft}
            disabled={disabled || pending || busy} onChange={(event) => onDraft(event.target.value)} />
          <button className={`icon-button microphone${voice.phase === 'recording' ? ' microphone--recording' : ''}`} type="button"
            aria-label={voice.phase === 'recording' ? '録音を停止' : '音声で回答'} aria-pressed={voice.phase === 'recording'}
            disabled={disabled || pending || !voice.mediaType || voice.phase === 'requesting' || voice.phase === 'stopping'}
            onClick={() => {
              if (voice.phase === 'recording') voice.stop()
              else { speech.stop(); void voice.start() }
            }}>
            <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><rect x="9" y="2" width="6" height="12" rx="3" /><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3m-4 0h8" /></svg>
          </button>
          <button className="icon-button filled" type="submit" aria-label="送信" disabled={!canSend}>
            <Icon name="send" />
          </button>
        </form>
        <p className="voice-notice">{voice.mediaType ? '音声は文字起こしのためGoogle Cloudへ送信されます。結果を確認してから送信できます。' : 'このブラウザでは音声入力を使えません。文字で入力してください。'}</p>
        {voice.recognized && <section className="transcript-preview" aria-label="文字起こし結果">
          <p>文字起こし結果を確認してください</p>
          <blockquote>{voice.recognized}</blockquote>
          <div>
            <button className="button filled" type="button" disabled={pending || busy} onClick={() => { onDraft(voice.recognized); voice.setRecognized('') }}>回答欄に入れる</button>
            {normalizeText(draft) && <button className="button outlined" type="button" disabled={pending || busy} onClick={() => { onDraft(`${normalizeText(draft)} ${voice.recognized}`); voice.setRecognized('') }}>回答の後ろに追加</button>}
            <button className="button text" type="button" onClick={() => voice.setRecognized('')}>破棄</button>
          </div>
        </section>}
        {overLimit && <p id="answer-error" className="field-error" role="alert">{format(ANSWER_LIMIT)}字以内にしてください</p>}
      </>}
    </footer>
  </>
}

export const checkItems = [
  '大事な点を自分の言葉で説明できた',
  '大事な言葉の意味を言えた',
  '理由やつながりを説明できた',
]

export function SelfCheck({
  checked, onCheck, onResume, onFinish, pending, error, canResume, disabled = false,
}: {
  checked: boolean[]
  onCheck: (index: number, checked: boolean) => void
  onResume: () => void
  onFinish: () => void
  pending: boolean
  error: string
  disabled?: boolean
  canResume: boolean
}) {
  return <>
    <main className="flow">
      <div className="message-list">
        <Message role="model" content="ここまでの説明に、納得できていますか？ 自分で確かめてみましょう。" />
      </div>
      <section className="checklist" role="group" aria-labelledby="check-title">
        <h2 id="check-title">説明できたもの</h2>
        {checkItems.map((text, index) => <label className="check-row" key={text}>
          <input type="checkbox" checked={checked[index] ?? false} disabled={pending}
            onChange={(event) => onCheck(index, event.target.checked)} />
          <span className="checkbox" aria-hidden="true"><Icon name="check" size={16} /></span>
          <span>{text}</span>
        </label>)}
      </section>
    </main>
    <footer className="bottom bottom--actions">
      {error && <Alert message={error} />}
      <button className="button outlined" type="button" disabled={pending || !canResume} onClick={onResume}>対話を続ける</button>
      <button className="button filled" type="button" disabled={pending || disabled} onClick={onFinish}>
        {pending ? <><Spinner />まとめています…</> : '振り返る'}
      </button>
    </footer>
  </>
}
