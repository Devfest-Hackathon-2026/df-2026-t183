import { useState } from 'react'
import type { ReactNode } from 'react'
import type { SavedResult, Summary, Turn } from '../../lib/api/learning'
import { checkItems } from './Conversation'
import type { IconName } from './icons'
import { Icon, Note, Zero } from './ui'

type ReviewKind = Summary['review'][number]['kind']

function ReviewTag({ kind }: { kind: ReviewKind }) {
  return kind === 'misunderstanding'
    ? <span className="tag tag--warning"><Icon name="warning" size={16} />誤解の可能性</span>
    : <span className="tag tag--unverified"><Icon name="help" size={16} />未確認</span>
}

function ResultCard({
  title, icon, count, empty, review, children,
}: { title: string; icon: IconName; count: number; empty: string; review?: boolean; children: ReactNode }) {
  return <section className={review ? 'result result--review' : 'result'}>
    <header>
      <span className="result-icon"><Icon name={icon} /></span>
      <h3>{title}</h3>
      <span className="badge">{count}<span className="visually-hidden">件</span></span>
    </header>
    {count === 0 ? <p className="empty">{empty}</p> : children}
  </section>
}

export function Reflection({
  result, history, checked, onHome, saved, homeLabel = 'ホームへ',
}: { homeLabel?: string; saved?: SavedResult; result: Summary; history: Turn[]; checked: boolean[]; onHome: () => void }) {
  const [copyMessage, setCopyMessage] = useState('')
  const firstAnswer = history[1]?.content
  const marked = checkItems.filter((_, index) => checked[index])
  return <>
    <main className="flow flow--reflection">
      <div className="done"><Zero size={56} /><h2>おつかれさまでした</h2></div>
      <ResultCard title="説明できた点" icon="spark" count={result.understood.length} empty="引用できる説明はありませんでした">
        {result.understood.map(({ point, evidence }, index) => <div className="result-item" key={index}>
          <h4>{point}</h4>
          <figure className="quote">
            <blockquote>「{evidence}」</blockquote>
            <figcaption>{firstAnswer?.includes(evidence) ? 'あなたの説明' : 'ゼロへの回答'}</figcaption>
          </figure>
        </div>)}
      </ResultCard>
      <ResultCard title="復習するとよい点" icon="description" count={result.review.length} empty="ありません" review>
        {result.review.map(({ kind, text }, index) => <div className="result-item" key={index}>
          <ReviewTag kind={kind} />
          <p>{text}</p>
        </div>)}
      </ResultCard>
      {marked.length > 0 && <details className="marked">
        <summary>自分のチェック（{marked.length}）<Icon name="expandMore" /></summary>
        <ul>{marked.map((item) => <li key={item}>{item}</li>)}</ul>
      </details>}
      {saved && <section className="card" aria-label="保存完了">
        <h3>クラウドに保存しました</h3>
        <p>閲覧キー <strong className="view-key">{saved.viewKey}</strong></p>
        <button className="button outlined" type="button" onClick={async () => {
          try {
            await navigator.clipboard.writeText(saved.viewKey)
            setCopyMessage('閲覧キーをコピーしました')
          } catch {
            setCopyMessage('コピーできませんでした。表示された閲覧キーを控えてください。')
          }
        }}>閲覧キーをコピー</button>
        <p role="status">{copyMessage}</p>
        <p>保存日時: {new Date(saved.savedAt).toLocaleString()}</p>
        <p>保存期限: {new Date(saved.expiresAt).toLocaleString()}</p>
        <Note>閲覧キーを控えてください。キーと保存済み結果はこの端末に保存しません。キーを知る人は記録を開けるため、他人に共有しないでください。</Note>
      </section>}
      {!saved && homeLabel === 'ホームへ' && <Note>この振り返りは保存されません</Note>}
    </main>
    <footer className="bottom bottom--actions">
      <button className="button filled" type="button" onClick={onHome}>{homeLabel}</button>
    </footer>
  </>
}
