import type { IconName } from './icons'
import { Icon, Message } from './ui'

const steps: [IconName, string][] = [['description', '教材'], ['forum', '説明'], ['taskAlt', '振り返り']]

export function Home({ onStart, onOpen }: { onStart: () => void; onOpen: () => void }) {
  return <main className="home">
    <header className="brand">
      <span className="brand-mark"><Icon name="chat" size={20} /></span>
      <span>Teach Back</span>
    </header>
    <div className="hero-demo" aria-hidden="true">
      <Message role="user" content="光で養分をつくる働きです。" />
      <Message role="model" content="「養分」って何ですか？" />
    </div>
    <section className="hero">
      <h1 tabIndex={-1}>教えて、わかる。</h1>
      <p>何も知らないAI「ゼロ」に説明して、理解を確かめる。</p>
    </section>
    <ol className="steps" aria-label="進め方">
      {steps.map(([icon, label]) => <li key={label}><span><Icon name={icon} /></span>{label}</li>)}
    </ol>
    <button type="button" className="preview-item" onClick={onOpen}>
      <Icon name="key" />
      <span>閲覧キーで開く</span>
    </button>
    <div className="home-cta">
      <button className="button filled large" type="button" onClick={onStart}>学習をはじめる</button>
    </div>
  </main>
}
