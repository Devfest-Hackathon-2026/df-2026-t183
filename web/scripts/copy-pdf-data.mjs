import { cp, mkdir } from 'node:fs/promises'

const destination = new URL('../public/pdfjs/', import.meta.url)
await mkdir(destination, { recursive: true })
await Promise.all(
  ['cmaps', 'standard_fonts', 'wasm'].map((directory) =>
    cp(new URL(`../node_modules/pdfjs-dist/${directory}/`, import.meta.url),
      new URL(directory, destination), { recursive: true }),
  ),
)
