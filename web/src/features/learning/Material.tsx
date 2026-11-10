import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { abortable, deadline } from '../../lib/abort'
import { FILE_LIMIT_MIB, MATERIAL_LIMIT, MIB, PAGE_LIMIT, normalizeText, textLength } from './limits'
import type { PageRange } from './pdf'
import { Alert, Icon, Note, Spinner } from './ui'

type Mode = 'direct' | 'pdf' | 'txt'
type FileMode = Exclude<Mode, 'direct'>
type ImportedFile = { name: string; size: number; pages?: number }
export type MaterialDraft = { mode: Mode; text: string; file?: ImportedFile }

const ttl = Number(import.meta.env.VITE_HISTORY_TTL_DAYS || 30)
const historyDays = Number.isInteger(ttl) && ttl > 0 ? ttl : 30

const modes = [['direct', '直接入力'], ['pdf', 'PDF'], ['txt', 'テキスト']] as const
const format = (number: number) => number.toLocaleString('ja-JP')
const formatSize = (bytes: number) => bytes < MIB ? `${format(Math.max(1, Math.round(bytes / 1024)))} KiB` :
  `${(bytes / MIB).toFixed(1)} MiB`

export function Material({
  draft, onChange, onStart, pending, error,
}: {
  draft: MaterialDraft
  onChange: (next: MaterialDraft) => void
  onStart: (text: string) => void
  pending: boolean
  error: string
}) {
  const input = useRef<HTMLInputElement>(null)
  const read = useRef<AbortController | null>(null)
  const [reading, setReading] = useState(false)
  const [fileError, setFileError] = useState('')
  const [pagePicker, setPagePicker] = useState<{ file: File; total: number }>()
  const { mode, text, file } = draft
  const count = textLength(text)
  const corrupt = Boolean(file && text.includes('�'))
  const overLimit = count > MATERIAL_LIMIT
  const replacementPending = Boolean(pagePicker || fileError)
  const fieldError = overLimit ? `${format(MATERIAL_LIMIT)}字以内にしてください` :
    corrupt ? '「�」は読み取りの誤りです。直してください' : ''

  useEffect(() => () => read.current?.abort(), [])

  function choose(next: Mode) {
    if (next === mode) return
    read.current?.abort()
    read.current = null
    setReading(false)
    setFileError('')
    setPagePicker(undefined)
    onChange({ mode: next, text })
  }

  async function readFile(selected: File, kind: FileMode, range?: PageRange) {
    if (read.current) return
    setFileError('')
    const controller = new AbortController()
    read.current = controller
    const timeout = deadline(controller.signal, 10_000)
    const { signal } = timeout
    setReading(true)
    try {
      if (selected.size > FILE_LIMIT_MIB * MIB) throw new Error('5 MiB 以下のファイルを選んでください。')
      if (!selected.name.toLowerCase().endsWith(`.${kind}`)) throw new Error(`拡張子が .${kind} のファイルを選んでください。`)
      const result = await abortable(signal, async (): Promise<{ text: string; pages?: number } | { totalPages: number }> => {
        const bytes = await selected.arrayBuffer()
        signal.throwIfAborted()
        if (kind === 'txt') {
          let text: string
          try {
            text = normalizeText(new TextDecoder('utf-8', { fatal: true }).decode(bytes))
          } catch {
            throw new Error('UTF-8 の .txt ファイルを選ぶか、本文を貼り付けてください。')
          }
          if (!text) throw new Error('ファイルに本文がありません。別のファイルを選んでください。')
          return { text }
        }
        try {
          const { extractPdf } = await import('./pdf')
          return await extractPdf(bytes, range, signal)
        } catch (cause) {
          if (cause instanceof Error && cause.message.startsWith('文字を読み取れませんでした。')) throw cause
          throw new Error('PDF を読み取れませんでした。別のファイルを選ぶか、本文を貼り付けてください。')
        }
      })
      signal.throwIfAborted()
      if ('totalPages' in result) {
        setPagePicker({ file: selected, total: result.totalPages })
        return
      }
      setPagePicker(undefined)
      onChange({ mode: kind, text: result.text, file: { name: selected.name, size: selected.size, pages: result.pages } })
    } catch (cause) {
      if (!controller.signal.aborted) {
        setFileError(timeout.timedOut ? '読み取りに時間がかかっています。ページを絞るか、本文を貼り付けてください。' :
          cause instanceof Error ? cause.message : 'ファイルを読み取れませんでした。')
      }
    } finally {
      timeout.dispose()
      if (read.current === controller) {
        read.current = null
        setReading(false)
      }
    }
  }

  function selectRange(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!pagePicker) return
    const fields = new FormData(event.currentTarget)
    const start = Number(fields.get('start'))
    const end = Number(fields.get('end'))
    if (end < start || end - start >= PAGE_LIMIT) {
      setFileError('読み取る範囲を20ページ以内にしてください。')
      return
    }
    void readFile(pagePicker.file, 'pdf', { start, end })
  }

  const fileMeta = file && [formatSize(file.size), file.pages === undefined ? '' : `${file.pages}ページ`, `${format(count)}字`]
    .filter(Boolean).join(' · ')

  return <>
    <main className="flow flow--material">
      <div className="segmented" role="group" aria-label="入力方法">
        {modes.map(([value, label]) => <button type="button" key={value} aria-pressed={mode === value}
          disabled={pending} onClick={() => choose(value)}>
          {mode === value && <Icon name="check" size={18} />}{label}
        </button>)}
      </div>
      {mode !== 'direct' && <input ref={input} type="file" hidden accept={mode === 'pdf' ? '.pdf,application/pdf' : '.txt,text/plain'}
        onChange={(event) => {
          const selected = event.currentTarget.files?.[0]
          event.currentTarget.value = ''
          if (selected) {
            setPagePicker(undefined)
            void readFile(selected, mode)
          }
        }} />}
      <p className="visually-hidden" role="status">{reading ? 'ファイルを読み込んでいます' : ''}</p>
      {!file && mode !== 'direct' && <button type="button" className="pick" disabled={reading} onClick={() => input.current?.click()}>
        <span className="pick-icon">{reading ? <Spinner /> : <Icon name="upload" />}</span>
        <span className="pick-label">{reading ? '読み込み中…' : mode === 'pdf' ? 'PDFを選ぶ' : 'テキストファイルを選ぶ'}</span>
        <span className="pick-meta">{mode === 'pdf' ? `${PAGE_LIMIT}ページ・${FILE_LIMIT_MIB} MiBまで` : `UTF-8・${FILE_LIMIT_MIB} MiBまで`}</span>
      </button>}
      {file && <section className="file" aria-label="読み込んだファイル">
        <span className="file-icon"><Icon name="description" /></span>
        <div className="file-text">
          <p className="file-name">{file.name}</p>
          <p className="file-meta">{fileMeta}</p>
        </div>
        <button type="button" className="icon-button" aria-label="別のファイルを選ぶ" disabled={pending || reading}
          onClick={() => input.current?.click()}>
          {reading ? <Spinner /> : <Icon name="swap" />}
        </button>
      </section>}
      {pagePicker && <form className="card range" onSubmit={selectRange}>
        <header>
          <h2>ページを選ぶ</h2>
          <p>全{pagePicker.total}ページ・最大{PAGE_LIMIT}ページ</p>
        </header>
        <div className="range-fields">
          <label className="mini-field"><span>開始</span>
            <input name="start" type="number" inputMode="numeric" min="1" max={pagePicker.total} defaultValue="1" required />
          </label>
          <label className="mini-field"><span>終了</span>
            <input name="end" type="number" inputMode="numeric" min="1" max={pagePicker.total}
              defaultValue={Math.min(PAGE_LIMIT, pagePicker.total)} required />
          </label>
        </div>
        <button type="submit" className="button tonal" disabled={reading}>{reading ? <Spinner /> : '読み取る'}</button>
      </form>}
      {(mode === 'direct' || file) && <div className={fieldError ? 'field field--error' : 'field'}>
        <div className="field-box">
          <label htmlFor="material">{file ? '読み取った本文' : '本文'}</label>
          <textarea id="material" value={text} disabled={pending || reading} spellCheck={false}
            aria-describedby="material-help" aria-invalid={Boolean(fieldError)}
            onChange={(event) => onChange({ ...draft, text: event.target.value })} />
        </div>
        <div className="field-support" id="material-help">
          {fieldError && <span>{fieldError}</span>}
          <span className="counter">{format(count)} / {format(MATERIAL_LIMIT)}</span>
        </div>
      </div>}
      {fileError && <Alert message={fileError} />}
      {error && <Alert message={error} />}
      <Note>本文はAIに送られます。個人情報は入れないでください。教材入力から下書きを最大2時間、学習開始後は教材・確定した対話・回答の下書きをこの端末に最大2時間一時保存し、再読み込み時に復元します。共有端末では終了時に破棄してください。振り返りを保存すると教材・対話・振り返りをクラウドに{historyDays}日間保存します。閲覧キーを知る人は記録を開けます。</Note>
    </main>
    <footer className="bottom bottom--actions">
      {file && replacementPending && <button className="button text" type="button" disabled={reading} onClick={() => {
        setPagePicker(undefined)
        setFileError('')
      }}>前の教材を使う</button>}
      <button className="button filled" type="button"
        disabled={pending || reading || replacementPending || !count || overLimit || corrupt || (mode !== 'direct' && !file)}
        onClick={() => onStart(normalizeText(text))}>
        {pending ? <><Spinner />準備中…</> : '説明をはじめる'}
      </button>
    </footer>
  </>
}
