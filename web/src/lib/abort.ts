export function deadline(parent: AbortSignal, milliseconds: number) {
  const controller = new AbortController()
  const forwardAbort = () => controller.abort(parent.reason)
  let timedOut = false
  const timer = setTimeout(() => {
    if (controller.signal.aborted) return
    timedOut = true
    controller.abort(new DOMException('Timed out', 'TimeoutError'))
  }, milliseconds)

  if (parent.aborted) forwardAbort()
  else parent.addEventListener('abort', forwardAbort, { once: true })

  return {
    signal: controller.signal,
    get timedOut() { return timedOut },
    dispose() {
      clearTimeout(timer)
      parent.removeEventListener('abort', forwardAbort)
    },
  }
}

export async function abortable<T>(signal: AbortSignal, work: () => Promise<T>): Promise<T> {
  signal.throwIfAborted()
  let abort!: () => void
  const cancelled = new Promise<never>((_, reject) => {
    abort = () => reject(signal.reason)
    signal.addEventListener('abort', abort, { once: true })
  })
  try {
    const running = Promise.resolve().then(() => {
      signal.throwIfAborted()
      return work()
    })
    return await Promise.race([running, cancelled])
  } finally {
    signal.removeEventListener('abort', abort)
  }
}
