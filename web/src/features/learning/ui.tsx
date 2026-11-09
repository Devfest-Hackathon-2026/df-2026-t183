import { useEffect, useRef } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import zeroAvatar from '../../assets/zero.svg'
import type { Turn } from '../../lib/api/learning'
import { icons } from './icons'
import type { IconName } from './icons'

export const STEP_COUNT = 5

export function Icon({ name, size = 24 }: { name: IconName; size?: number }) {
  return <svg className="icon" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    <path d={icons[name]} />
  </svg>
}

export function Zero({ size = 32 }: { size?: number }) {
  return <img className="zero" alt="" src={zeroAvatar} width={size} height={size} />
}

export function Spinner() {
  return <span className="spinner" aria-hidden="true" />
}

export function TopBar({ title, step, onBack }: { title: string; step: number; onBack: () => void }) {
  return <header className="top-bar">
    <div className="top-bar-row">
      <button className="icon-button" type="button" aria-label="戻る" onClick={onBack}>
        <Icon name="arrowBack" />
      </button>
      <h1 tabIndex={-1}>{title}</h1>
    </div>
    <div className="progress" role="progressbar" aria-label="進み具合" aria-valuemin={1} aria-valuemax={STEP_COUNT}
      aria-valuenow={step} aria-valuetext={`${step}/${STEP_COUNT}`}>
      <span style={{ '--progress': step / STEP_COUNT } as CSSProperties} />
    </div>
  </header>
}

export function Alert({ message }: { message: string }) {
  return <p className="alert" role="alert"><Icon name="error" size={20} /><span>{message}</span></p>
}

export function Note({ children }: { children: ReactNode }) {
  return <p className="note"><Icon name="info" size={18} /><span>{children}</span></p>
}

export function Message({ role, content, speech }: Turn & { speech?: {
  phase: 'idle' | 'loading' | 'ready' | 'playing'; disabled: boolean; onPlay: () => void; onStop: () => void
} }) {
  return <div className={`message message--${role}`}>
    {role === 'model' && <Zero />}
    <p className="bubble"><span className="visually-hidden">{role === 'model' ? 'ゼロ：' : 'あなた：'}</span>{content}</p>
    {speech && <button type="button" className="icon-button speech-button"
      aria-label={speech.phase === 'playing' ? '質問の読み上げを停止' : '質問を読み上げる'}
      disabled={speech.disabled || speech.phase === 'loading'}
      onClick={speech.phase === 'playing' ? speech.onStop : speech.onPlay}>
      {speech.phase === 'loading' ? <Spinner /> : <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        {speech.phase === 'playing' ? <rect x="6" y="6" width="12" height="12" rx="1" /> : <><path d="M4 9v6h4l5 4V5L8 9H4Z" /><path d="M17 9a5 5 0 0 1 0 6M20 6a9 9 0 0 1 0 12" /></>}
      </svg>}
    </button>}
  </div>
}

export function Thinking() {
  return <div className="message message--model">
    <Zero />
    <p className="bubble bubble--typing">
      <span className="visually-hidden">ゼロが考えています</span>
      <span className="dot" aria-hidden="true" /><span className="dot" aria-hidden="true" /><span className="dot" aria-hidden="true" />
    </p>
  </div>
}

export type Confirmation = { title: string; body: string; confirm: string }

export function ConfirmDialog({ title, body, confirm, onClose }: Confirmation & { onClose: (ok: boolean) => void }) {
  const dialog = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const element = dialog.current
    if (element && !element.open) element.showModal()
  }, [])

  function close(ok: boolean) {
    dialog.current?.close()
    onClose(ok)
  }

  return <dialog ref={dialog} className="dialog" aria-labelledby="dialog-title" aria-describedby="dialog-body"
    onCancel={(event) => {
      event.preventDefault()
      close(false)
    }}
    onClick={(event) => {
      if (event.target === event.currentTarget) close(false)
    }}>
    <div className="dialog-body">
      <h2 id="dialog-title">{title}</h2>
      <p id="dialog-body">{body}</p>
      <div className="dialog-actions">
        <button type="button" className="button text" onClick={() => close(false)}>キャンセル</button>
        <button type="button" className="button text" onClick={() => close(true)}>{confirm}</button>
      </div>
    </div>
  </dialog>
}
