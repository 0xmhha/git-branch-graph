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
