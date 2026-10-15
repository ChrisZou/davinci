import { Zip, ZipPassThrough } from 'fflate'
import type { Board } from '../types'
import type { EncodeOptions } from './ck'

/** A portable filename stem, also safe as a ZIP entry (never a path). */
export function exportName(name: string, fallback = 'davinci'): string {
  let safe = name.normalize('NFC').replace(/[<>:"/\\|?*\x00-\x1f\x7f]/g, '_').trim().replace(/^\.+/, '')
  // Leave room for a duplicate suffix and extension, including on UTF-8 filesystems.
  const encoder = new TextEncoder()
  let bytes = 0
  safe = Array.from(safe).filter((char) => (bytes += encoder.encode(char).length) <= 180).join('').replace(/[. ]+$/, '')
  if (!safe) return fallback
  if (/^(con|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³])(?:\.|$)/i.test(safe)) safe = `_${safe}`
  return safe
}

/** Render sequentially; already compressed images only need ZIP storage. */
export async function exportBoards(
  boards: Board[],
  options: Omit<EncodeOptions, 'ids'>,
  encode: (board: Board, options: EncodeOptions) => Promise<Blob>,
): Promise<Blob> {
  const parts: BlobPart[] = []
  const zip = new Zip((error, chunk) => {
    if (error) throw error
    parts.push(new Blob([chunk as BlobPart]))
  })
  const taken = new Set<string>()
  const extension = options.format === 'jpeg' ? 'jpg' : options.format ?? 'png'
  // Snapshot before awaiting resources so remote edits cannot mix revisions.
  const snapshot = structuredClone(boards)
  try {
    for (const [index, board] of snapshot.entries()) {
      const base = exportName(board.name, `画板 ${index + 1}`)
      let stem = base
      for (let n = 2; taken.has(stem.toLowerCase()); n++) stem = `${base} (${n})`
      taken.add(stem.toLowerCase())
      const blob = await encode(board, { ...options, ids: undefined })
      const entry = new ZipPassThrough(`${stem}.${extension}`)
      zip.add(entry)
      entry.push(new Uint8Array(await blob.arrayBuffer()), true)
    }
    zip.end()
    return new Blob(parts, { type: 'application/zip' })
  } catch (error) {
    zip.terminate()
    throw error
  }
}
