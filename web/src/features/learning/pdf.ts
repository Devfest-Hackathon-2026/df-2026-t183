import 'core-js/actual/promise/with-resolvers.js'
import { getDocument, PDFWorker } from 'pdfjs-dist/legacy/build/pdf.mjs'
import { abortable } from '../../lib/abort'
import { MATERIAL_LIMIT, normalizeText, PAGE_LIMIT } from './limits'

export type PageRange = { start: number; end: number }

export async function extractPdf(bytes: ArrayBuffer, range: PageRange | undefined, signal: AbortSignal) {
  signal.throwIfAborted()
  const thread = new Worker(new URL('./pdf.worker.ts', import.meta.url), { type: 'module' })
  try {
    const worker = PDFWorker.create({ port: thread })
    try {
      return await abortable(signal, () => readPdf(worker, bytes, range, signal))
    } finally {
      worker.destroy()
    }
  } finally {
    thread.terminate()
  }
}

async function readPdf(worker: PDFWorker, bytes: ArrayBuffer, range: PageRange | undefined, signal: AbortSignal) {
  const dataUrl = new URL(`${import.meta.env.BASE_URL}pdfjs/`, window.location.href)
  const task = getDocument({
    data: bytes,
    worker,
    disableWorkerRendering: true,
    cMapUrl: new URL('cmaps/', dataUrl).href,
    cMapPacked: true,
    standardFontDataUrl: new URL('standard_fonts/', dataUrl).href,
    wasmUrl: new URL('wasm/', dataUrl).href,
    disableFontFace: true,
    stopAtErrors: true,
  })
  try {
    const pdf = await task.promise
    signal.throwIfAborted()
    if (!range && pdf.numPages > PAGE_LIMIT) return { totalPages: pdf.numPages }
    const { start, end } = range ?? { start: 1, end: pdf.numPages }
    if (!Number.isInteger(start) || !Number.isInteger(end) ||
      start < 1 || start > end || end > pdf.numPages || end - start >= PAGE_LIMIT) {
      return { totalPages: pdf.numPages }
    }
    const pages: string[] = []
    for (let number = start; number <= end; number += 1) {
      signal.throwIfAborted()
      const page = await pdf.getPage(number)
      try {
        const content = await page.getTextContent()
        const text = content.items.map((item) =>
          'str' in item ? item.str + (item.hasEOL ? '\n' : '') : '',
        ).join('')
        pages.push(normalizeText(text))
      } finally {
        page.cleanup()
      }
    }
    const text = normalizeText(pages.filter(Boolean).join('\n\n'))
    if (!text) throw new Error('文字を読み取れませんでした。別のページを選ぶか、本文を貼り付けてください。')
    if (Array.from(text).length > MATERIAL_LIMIT && !range && pdf.numPages > 1) {
      return { totalPages: pdf.numPages }
    }
    return { text, pages: end - start + 1 }
  } finally {
    if (!signal.aborted) await task.destroy()
  }
}
