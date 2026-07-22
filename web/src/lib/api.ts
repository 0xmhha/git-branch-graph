import type { RunSummary, Graph, Containment } from './types'
import * as sdb from './staticdb'

// The same build runs two ways: served by `gbg serve` (has /api), or as a static
// export (files + browser sql.js). Mode is detected once by probing /api/runs.
let modeP: Promise<'server' | 'static'> | null = null
export function mode(): Promise<'server' | 'static'> {
  if (!modeP) {
    modeP = (async () => {
      try {
        const r = await fetch('/api/runs', { cache: 'no-store' })
        if (r.ok) return 'server'
      } catch {
        /* static */
      }
      return 'static'
    })()
  }
  return modeP
}

export async function fetchRuns(): Promise<RunSummary[]> {
  if ((await mode()) === 'server') {
    const r = await fetch('/api/runs')
    if (!r.ok) throw new Error(`runs: ${r.status}`)
    return r.json()
  }
  const r = await fetch('./runs.json')
  if (!r.ok) throw new Error(`runs: ${r.status}`)
  return r.json()
}

export async function fetchGraph(id: string): Promise<Graph> {
  const base =
    (await mode()) === 'server'
      ? `/api/runs/${encodeURIComponent(id)}/graph.json`
      : `./data/${encodeURIComponent(id)}/graph.json`
  const r = await fetch(base)
  if (!r.ok) throw new Error(`graph: ${r.status}`)
  return r.json()
}

// Server-side SQLite query (server mode) or in-browser sql.js (static mode).
export async function fetchContainment(id: string, sha: string): Promise<Containment> {
  return fetchContainmentRef(id, { sha })
}

export interface PRRow {
  num: number
  state: string
  mergeMethod: string
  baseRef: string
  headRef: string
  url: string
}

export async function fetchPRs(
  id: string,
  opts: { method?: string; state?: string; limit?: number } = {},
): Promise<PRRow[]> {
  if ((await mode()) === 'static') return sdb.prs(id, opts)
  const p = new URLSearchParams()
  if (opts.method) p.set('method', opts.method)
  if (opts.state) p.set('state', opts.state)
  if (opts.limit) p.set('limit', String(opts.limit))
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/prs?${p}`)
  if (!r.ok) throw new Error(`prs: ${r.status}`)
  return r.json()
}

export interface DiffCommit {
  sha: string
  subject: string
  prNum: string
  committedAt: string
}

export async function fetchDiff(id: string, inRef: string, notinRef: string): Promise<{ count: number; commits: DiffCommit[] }> {
  if ((await mode()) === 'static') return sdb.diff(id, inRef, notinRef)
  const p = new URLSearchParams({ in: inRef, notin: notinRef, limit: '500' })
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/diff?${p}`)
  if (!r.ok) throw new Error(`diff: ${r.status}`)
  return r.json()
}

export interface Release {
  family: string
  date: string
  mainTag: string
  envs: Record<string, { tag: string; sha: string }>
}

export async function fetchReleases(id: string): Promise<{ environments: string[]; releases: Release[] }> {
  if ((await mode()) === 'static') return sdb.releases(id)
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/releases`)
  if (!r.ok) throw new Error(`releases: ${r.status}`)
  return r.json()
}

// Reverse lookup — accepts a commit SHA or a PR number.
export async function fetchContainmentRef(id: string, ref: { sha?: string; pr?: string }): Promise<Containment> {
  if ((await mode()) === 'static') return sdb.containment(id, ref)
  const p = new URLSearchParams()
  if (ref.sha) p.set('sha', ref.sha)
  if (ref.pr) p.set('pr', ref.pr)
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/containment?${p}`)
  if (!r.ok) throw new Error(`containment: ${r.status}`)
  return r.json()
}
