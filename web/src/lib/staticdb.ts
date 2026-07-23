// Static-mode data layer: when there is no gbg server, query the run's
// graph.sqlite in the browser with sql.js (WASM). Mirrors the server endpoints
// so the rest of the app is agnostic to which mode it runs in.
import initSqlJs, { type Database } from 'sql.js'
import type { Containment } from './types'
import type { PRRow, DiffCommit, Release } from './api'

let sqlP: Promise<any> | null = null
const dbCache = new Map<string, Promise<Database>>()

function getDB(runId: string): Promise<Database> {
  let p = dbCache.get(runId)
  if (!p) {
    p = (async () => {
      if (!sqlP) sqlP = initSqlJs({ locateFile: (f: string) => `./${f}` })
      const SQL = await sqlP
      const buf = await (await fetch(`./data/${encodeURIComponent(runId)}/graph.sqlite`)).arrayBuffer()
      return new SQL.Database(new Uint8Array(buf))
    })()
    dbCache.set(runId, p)
  }
  return p
}

function query(db: Database, sql: string, params: unknown[] = []): Record<string, unknown>[] {
  const stmt = db.prepare(sql)
  stmt.bind(params as never)
  const out: Record<string, unknown>[] = []
  while (stmt.step()) out.push(stmt.getAsObject())
  stmt.free()
  return out
}

export async function containment(runId: string, ref: { sha?: string; pr?: string }): Promise<Containment> {
  const db = await getDB(runId)
  let sha = ref.sha ?? ''
  if (!sha && ref.pr) {
    const r = query(db, `SELECT merge_sha FROM prs WHERE pr_num=?`, [ref.pr])
    sha = (r[0]?.merge_sha as string) ?? ''
  }
  if (!sha) return { sha: '', branches: [], tags: [] }
  const rows = query(
    db,
    `SELECT r.ref_name AS name, r.ref_type AS type
     FROM containment ct JOIN commits c ON c.id=ct.commit_id JOIN refs r ON r.id=ct.ref_id
     WHERE c.sha=? ORDER BY r.ref_type, r.ref_name`,
    [sha],
  )
  return {
    sha,
    tags: rows.filter((r) => r.type === 'tag').map((r) => ({ name: r.name as string, type: 'tag' })),
    branches: rows.filter((r) => r.type === 'branch').map((r) => ({ name: r.name as string, type: 'branch' })),
  }
}

export async function prs(runId: string, opts: { method?: string; state?: string; limit?: number }): Promise<PRRow[]> {
  const db = await getDB(runId)
  const rows = query(
    db,
    `SELECT pr_num, state, merge_method, base_ref, head_ref, url FROM prs
     WHERE (?1='' OR merge_method=?1) AND (?2='' OR state=?2)
     ORDER BY pr_num DESC LIMIT ?3`,
    [opts.method ?? '', opts.state ?? '', opts.limit ?? 200],
  )
  return rows.map((r) => ({
    num: Number(r.pr_num),
    state: (r.state as string) ?? '',
    mergeMethod: (r.merge_method as string) ?? '',
    baseRef: (r.base_ref as string) ?? '',
    headRef: (r.head_ref as string) ?? '',
    url: (r.url as string) ?? '',
  }))
}

export async function diff(runId: string, inRef: string, notinRef: string): Promise<{ count: number; commits: DiffCommit[] }> {
  const db = await getDB(runId)
  // Older graph.sqlite files predate the unpushed column — select 0 instead.
  const hasUnpushed =
    ((query(db, `SELECT COUNT(*) AS n FROM pragma_table_info('commits') WHERE name='unpushed'`)[0]?.n as number) ?? 0) > 0
  const rows = query(
    db,
    `SELECT c.sha, c.subject, c.pr_num, c.committed_at, ${hasUnpushed ? 'c.unpushed' : '0'} AS unpushed FROM commits c
     WHERE EXISTS (SELECT 1 FROM containment ct JOIN refs r ON r.id=ct.ref_id WHERE ct.commit_id=c.id AND r.ref_name=?1)
       AND NOT EXISTS (SELECT 1 FROM containment ct JOIN refs r ON r.id=ct.ref_id WHERE ct.commit_id=c.id AND r.ref_name=?2)
     ORDER BY c.committed_at DESC LIMIT 500`,
    [inRef, notinRef],
  )
  const commits: DiffCommit[] = rows.map((r) => ({
    sha: r.sha as string,
    subject: (r.subject as string) ?? '',
    prNum: r.pr_num != null ? String(r.pr_num) : '',
    committedAt: (r.committed_at as string) ?? '',
    unpushed: !!(r.unpushed as number),
  }))
  return { count: commits.length, commits }
}

// splitVersion mirrors the Go handler: trailing "_<letters…>" is an environment.
function splitVersion(tag: string): { family: string; env: string } {
  const m = tag.match(/^(.*?)_([A-Za-z][A-Za-z0-9_]*)$/)
  return m ? { family: m[1], env: m[2] } : { family: tag, env: 'release' }
}

export async function releases(runId: string): Promise<{ environments: string[]; releases: Release[] }> {
  const db = await getDB(runId)
  const rows = query(
    db,
    `SELECT r.ref_name AS name, r.target_sha AS sha, COALESCE(c.committed_at,'') AS date
     FROM refs r LEFT JOIN commits c ON c.sha=r.target_sha WHERE r.ref_type='tag'`,
  )
  const fams = new Map<string, Release>()
  const envSet = new Set<string>()
  for (const row of rows) {
    const name = row.name as string
    const sha = (row.sha as string) ?? ''
    const date = (row.date as string) ?? ''
    const { family, env } = splitVersion(name)
    envSet.add(env)
    let f = fams.get(family)
    if (!f) {
      f = { family, date: '', mainTag: '', envs: {} }
      fams.set(family, f)
    }
    f.envs[env] = { tag: name, sha }
    if (date > f.date) f.date = date
    if (env === 'release' || !f.mainTag) f.mainTag = name
  }
  const environments = ['release', ...[...envSet].filter((e) => e !== 'release').sort()]
  const list = [...fams.values()].sort((a, b) => (a.date !== b.date ? (a.date > b.date ? -1 : 1) : a.family > b.family ? -1 : 1))
  return { environments, releases: list }
}
