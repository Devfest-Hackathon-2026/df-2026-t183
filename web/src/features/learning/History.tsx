import { useEffect, useRef, useState } from 'react'
import { openHistory } from '../../lib/api/learning'
import type { HistoryRecord } from '../../lib/api/learning'
import { Reflection } from './Reflection'
import { Alert, Message, Spinner } from './ui'

export function History() {
  const [key, setKey] = useState('')
  const [record, setRecord] = useState<HistoryRecord>()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  if (record) return <>
    <section className="flow">
      <h1 tabIndex={-1}>保存した学習記録</h1>
      <p>保存日時: {new Date(record.savedAt).toLocaleString()}</p>
      <p>保存期限: {new Date(record.expiresAt).toLocaleString()}</p>
      <details><summary>教材</summary><p style={{ whiteSpace: 'pre-wrap' }}>{record.learningContent}</p></details>
      <details><summary>対話</summary>{record.history.map((turn, i) => <Message key={i} {...turn} />)}</details>
    </section>
    <Reflection homeLabel="別の記録を開く" result={record.summary} history={record.history} checked={[]} onHome={() => { setRecord(undefined); setKey('') }} />
  </>
  return <main className="flow">
    <h1 tabIndex={-1}>閲覧キーで開く</h1>
    <p>キーはこのブラウザに保存しません。期限切れの記録は開けません。</p>
    <form onSubmit={async event => {
      event.preventDefault()
      if (request.current || !key.trim()) return
      const controller = new AbortController()
      request.current = controller
      setPending(true)
      setError('')
      try {
        const result = await openHistory(key, controller.signal)
        if (!controller.signal.aborted) { setRecord(result); setKey('') }
      } catch (cause) {
        if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : '記録を開けませんでした。')
      } finally {
        if (request.current === controller) { request.current = null; setPending(false) }
      }
    }}>
      <div className="field"><div className="field-box">
        <label htmlFor="view-key">閲覧キー</label>
        <input id="view-key" value={key} onChange={event => setKey(event.target.value)} autoComplete="off" autoCapitalize="characters" spellCheck={false} maxLength={64} disabled={pending} />
      </div></div>
      {error && <Alert message={error} />}
      <button className="button filled" type="submit" disabled={pending || !key.trim()}>{pending ? <Spinner /> : '記録を開く'}</button>
    </form>
  </main>
}
