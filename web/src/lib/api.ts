import type { RunSummary, Graph, Containment } from './types'

export async function fetchRuns(): Promise<RunSummary[]> {
  const r = await fetch('/api/runs')
  if (!r.ok) throw new Error(`runs: ${r.status}`)
  return r.json()
}

export async function fetchGraph(id: string): Promise<Graph> {
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/graph.json`)
  if (!r.ok) throw new Error(`graph: ${r.status}`)
  return r.json()
}

// Server-side SQLite query — the browser never loads graph.sqlite.
export async function fetchContainment(id: string, sha: string): Promise<Containment> {
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/containment?sha=${sha}`)
  if (!r.ok) throw new Error(`containment: ${r.status}`)
  return r.json()
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
  const p = new URLSearchParams({ in: inRef, notin: notinRef, limit: '500' })
  const r = await fetch(`/api/runs/${encodeURIComponent(id)}/diff?${p}`)
  if (!r.ok) throw new Error(`diff: ${r.status}`)
  return r.json()
}
