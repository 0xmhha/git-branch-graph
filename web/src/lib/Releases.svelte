<script lang="ts">
  import { fetchReleases, fetchDiff, fetchContainmentRef, type Release, type DiffCommit } from './api'
  import type { Graph, Containment } from './types'

  let { graph, runId }: { graph: Graph; runId: string } = $props()

  let environments = $state<string[]>([])
  let releases = $state<Release[]>([])
  let err = $state('')
  let filter = $state('')

  // ② per-release contents (lazy) — commits added vs the previous release.
  let expanded = $state<string>('')
  const contents = new Map<string, DiffCommit[]>()
  let contentsView = $state<DiffCommit[] | null>(null)

  // ③ next-release preview
  let nextPreview = $state<{ base: string; count: number; commits: DiffCommit[] } | null>(null)

  // ④ reverse lookup
  let lookup = $state('')
  let lookupRes = $state<Containment | null>(null)
  let lookupErr = $state('')

  const filtered = $derived(
    filter.trim() ? releases.filter((r) => r.family.toLowerCase().includes(filter.trim().toLowerCase())) : releases,
  )

  $effect(() => {
    fetchReleases(runId)
      .then((d) => {
        environments = d.environments
        releases = d.releases
        if (d.releases.length) loadNextPreview(d.releases[0].mainTag)
      })
      .catch((e) => (err = String(e)))
  })

  async function loadNextPreview(latestTag: string) {
    try {
      const d = await fetchDiff(runId, graph.meta.defaultBranch, latestTag)
      nextPreview = { base: latestTag, count: d.count, commits: d.commits }
    } catch {
      /* ignore */
    }
  }

  async function toggle(i: number) {
    const r = filtered[i]
    if (expanded === r.family) {
      expanded = ''
      contentsView = null
      return
    }
    expanded = r.family
    if (contents.has(r.family)) {
      contentsView = contents.get(r.family)!
      return
    }
    // previous release in the full (date-desc) list
    const idx = releases.findIndex((x) => x.family === r.family)
    const prev = releases[idx + 1]
    contentsView = null
    if (!prev) {
      contents.set(r.family, [])
      contentsView = []
      return
    }
    try {
      const d = await fetchDiff(runId, r.mainTag, prev.mainTag)
      contents.set(r.family, d.commits)
      if (expanded === r.family) contentsView = d.commits
    } catch {
      contentsView = []
    }
  }

  async function runLookup() {
    lookupErr = ''
    lookupRes = null
    const v = lookup.trim().replace(/^#/, '')
    if (!v) return
    const ref = /^\d+$/.test(v) ? { pr: v } : { sha: v }
    try {
      lookupRes = await fetchContainmentRef(runId, ref)
    } catch (e) {
      lookupErr = String(e)
    }
  }

  const linkBase = $derived(graph.linkBase)
  const tagUrl = (t: string) => `${linkBase}/releases/tag/${t}`
</script>

<div class="h-full overflow-auto px-5 py-4 max-w-5xl">
  {#if err}
    <p class="text-sm text-red-600">Error: {err}</p>
  {/if}

  <!-- ④ reverse lookup -->
  <section class="mb-5">
    <h2 class="text-xs uppercase tracking-wide text-neutral-400 mb-2">Where is this fix?</h2>
    <div class="flex gap-2 items-center">
      <input
        class="w-72 px-2.5 py-1.5 text-sm font-mono rounded-md border border-neutral-300 dark:border-neutral-700 bg-transparent outline-none focus:border-emerald-500"
        placeholder="commit SHA or PR #"
        bind:value={lookup}
        onkeydown={(e) => e.key === 'Enter' && runLookup()}
      />
      <button class="text-xs px-3 py-1.5 rounded-md bg-emerald-600 text-white" onclick={runLookup}>Find</button>
      {#if lookupErr}<span class="text-xs text-red-600">{lookupErr}</span>{/if}
    </div>
    {#if lookupRes}
      <div class="mt-2 text-xs text-neutral-600 dark:text-neutral-300">
        <span class="font-mono text-neutral-400">{lookupRes.sha.slice(0, 9)}</span>
        {#if lookupRes.tags && lookupRes.tags.length}
          <span class="ml-2">in releases:</span>
          {#each lookupRes.tags as t}
            <a class="ml-1 text-emerald-600 hover:underline" href={tagUrl(t.name)} target="_blank" rel="noopener">{t.name}</a>
          {/each}
        {:else}
          <span class="ml-2 italic text-neutral-400">in no release tag yet</span>
        {/if}
        {#if lookupRes.branches && lookupRes.branches.length}
          <span class="ml-3 text-neutral-400">branches:</span>
          <span class="ml-1">{lookupRes.branches.map((b) => b.name).join(', ')}</span>
        {/if}
      </div>
    {/if}
  </section>

  <!-- ③ next release preview -->
  {#if nextPreview}
    <section class="mb-5 rounded-lg border border-neutral-200 dark:border-neutral-800 p-3">
      <div class="flex items-baseline gap-2">
        <h2 class="text-sm font-semibold">Unreleased on {graph.meta.defaultBranch}</h2>
        <span class="text-xs text-neutral-400">{nextPreview.count} commits since {nextPreview.base}</span>
      </div>
      {#if nextPreview.count}
        <ul class="mt-2 flex flex-col gap-0.5 max-h-40 overflow-auto">
          {#each nextPreview.commits.slice(0, 40) as c}
            <li class="text-xs leading-tight text-neutral-600 dark:text-neutral-300">
              <a class="font-mono text-[10px] text-emerald-600" href={`${linkBase}/commit/${c.sha}`} target="_blank" rel="noopener">{c.sha.slice(0, 8)}</a>
              {#if c.prNum}<span class="text-neutral-400">#{c.prNum}</span>{/if}
              <span>{c.subject}</span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="mt-1 text-xs text-neutral-400 italic">{graph.meta.defaultBranch} is fully released.</p>
      {/if}
    </section>
  {/if}

  <!-- ① environment matrix (+ ② expandable contents) -->
  <section>
    <div class="flex items-baseline justify-between mb-2">
      <h2 class="text-sm font-semibold">Releases × environments</h2>
      <input
        class="w-40 px-2 py-1 text-xs rounded-md border border-neutral-300 dark:border-neutral-700 bg-transparent outline-none"
        placeholder="filter version…"
        bind:value={filter}
      />
    </div>
    <div class="overflow-x-auto rounded-lg border border-neutral-200 dark:border-neutral-800">
      <table class="w-full text-sm border-collapse">
        <thead>
          <tr class="text-left text-xs text-neutral-500 border-b border-neutral-200 dark:border-neutral-800">
            <th class="px-3 py-2 font-medium">Version</th>
            <th class="px-3 py-2 font-medium">Date</th>
            {#each environments as env}
              <th class="px-3 py-2 font-medium capitalize text-center">{env}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each filtered as r, i (r.family)}
            <tr
              class="border-b border-neutral-100 dark:border-neutral-800/60 hover:bg-neutral-50 dark:hover:bg-neutral-900 cursor-pointer"
              onclick={() => toggle(i)}
            >
              <td class="px-3 py-1.5 font-mono font-medium whitespace-nowrap">
                <span class="text-neutral-400 mr-1">{expanded === r.family ? '▾' : '▸'}</span>{r.family}
              </td>
              <td class="px-3 py-1.5 text-xs text-neutral-400 tabular-nums whitespace-nowrap">{r.date.slice(0, 10)}</td>
              {#each environments as env}
                <td class="px-3 py-1.5 text-center">
                  {#if r.envs[env]}
                    <a
                      class="text-emerald-600 hover:underline"
                      href={tagUrl(r.envs[env].tag)}
                      target="_blank"
                      rel="noopener"
                      onclick={(e) => e.stopPropagation()}
                      title={r.envs[env].tag}
                    >✓</a>
                  {:else}
                    <span class="text-neutral-300 dark:text-neutral-700">—</span>
                  {/if}
                </td>
              {/each}
            </tr>
            {#if expanded === r.family}
              <tr class="bg-neutral-50 dark:bg-neutral-900/50">
                <td colspan={2 + environments.length} class="px-3 py-2">
                  {#if contentsView === null}
                    <span class="text-xs text-neutral-400">Loading…</span>
                  {:else if contentsView.length === 0}
                    <span class="text-xs text-neutral-400 italic">No prior release to diff against.</span>
                  {:else}
                    <div class="text-xs text-neutral-500 mb-1">Added in {r.family} — {contentsView.length} commits</div>
                    <ul class="flex flex-col gap-0.5 max-h-52 overflow-auto">
                      {#each contentsView as c}
                        <li class="text-xs leading-tight">
                          <a class="font-mono text-[10px] text-emerald-600" href={`${linkBase}/commit/${c.sha}`} target="_blank" rel="noopener">{c.sha.slice(0, 8)}</a>
                          {#if c.prNum}<a class="text-neutral-400 hover:underline" href={`${linkBase}/pull/${c.prNum}`} target="_blank" rel="noopener">#{c.prNum}</a>{/if}
                          <span class="text-neutral-600 dark:text-neutral-300">{c.subject}</span>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    </div>
  </section>
</div>
