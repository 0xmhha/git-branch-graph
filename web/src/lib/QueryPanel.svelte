<script lang="ts">
  import { fetchPRs, fetchDiff, type PRRow, type DiffCommit } from './api'
  import type { Graph } from './types'

  let { graph, runId }: { graph: Graph; runId: string } = $props()

  type Tab = 'diff' | 'prs'
  let tab = $state<Tab>('diff')

  // Reverse query: commits in one ref not yet in another (release readiness).
  const branches = $derived(graph.nodes.flatMap((n) => (n.refs ?? []).filter((r) => r.type === 'branch').map((r) => r.name)))
  let inRef = $state('')
  let notinRef = $state('')
  let diff = $state<{ count: number; commits: DiffCommit[] } | null>(null)
  let diffErr = $state('')

  $effect(() => {
    // default the two refs to default branch vs a release branch if present
    if (!inRef && branches.length) {
      inRef = graph.meta.defaultBranch
      notinRef = branches.find((b) => b.startsWith('release/')) ?? branches.find((b) => b !== inRef) ?? ''
    }
  })

  async function runDiff() {
    diffErr = ''
    diff = null
    try {
      diff = await fetchDiff(runId, inRef, notinRef)
    } catch (e) {
      diffErr = String(e)
    }
  }

  // PRs by merge method.
  let method = $state('squash')
  let prs = $state<PRRow[]>([])
  let prErr = $state('')
  async function runPRs() {
    prErr = ''
    try {
      prs = await fetchPRs(runId, { method, state: 'merged', limit: 200 })
    } catch (e) {
      prErr = String(e)
    }
  }

  const linkBase = $derived(graph.linkBase)
</script>

<aside class="w-80 shrink-0 border-l border-neutral-200 dark:border-neutral-800 flex flex-col text-sm">
  <div class="flex border-b border-neutral-200 dark:border-neutral-800">
    <button
      class="flex-1 px-3 py-2 text-xs font-medium {tab === 'diff' ? 'border-b-2 border-emerald-500' : 'text-neutral-500'}"
      onclick={() => (tab = 'diff')}
    >Release diff</button>
    <button
      class="flex-1 px-3 py-2 text-xs font-medium {tab === 'prs' ? 'border-b-2 border-emerald-500' : 'text-neutral-500'}"
      onclick={() => (tab = 'prs')}
    >PRs by method</button>
  </div>

  {#if tab === 'diff'}
    <div class="p-3 flex flex-col gap-2">
      <p class="text-xs text-neutral-500">Commits in one ref, not yet in another.</p>
      <label class="text-xs">in
        <select bind:value={inRef} class="w-full mt-0.5 bg-transparent border border-neutral-300 dark:border-neutral-700 rounded px-1.5 py-1">
          {#each branches as b}<option value={b}>{b}</option>{/each}
        </select>
      </label>
      <label class="text-xs">not in
        <select bind:value={notinRef} class="w-full mt-0.5 bg-transparent border border-neutral-300 dark:border-neutral-700 rounded px-1.5 py-1">
          {#each branches as b}<option value={b}>{b}</option>{/each}
        </select>
      </label>
      <button class="self-start px-2.5 py-1 text-xs rounded bg-emerald-600 text-white" onclick={runDiff}>Run</button>
      {#if diffErr}<p class="text-xs text-red-600">{diffErr}</p>{/if}
      {#if diff}
        <p class="text-xs text-neutral-500">{diff.count} commit{diff.count === 1 ? '' : 's'} in <b>{inRef}</b> not in <b>{notinRef}</b></p>
        <ul class="flex flex-col gap-1 overflow-auto">
          {#each diff.commits as c}
            <li class="text-xs leading-tight">
              {#if c.unpushed}
                <span class="font-mono text-[10px] text-amber-600" title="Local-only commit — not pushed to the remote">{c.sha.slice(0, 8)} ⇡local</span>
              {:else}
                <a class="font-mono text-[10px] text-emerald-600" href={`${linkBase}/commit/${c.sha}`} target="_blank" rel="noopener">{c.sha.slice(0, 8)}</a>
              {/if}
              <span class="text-neutral-500">{c.subject}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {:else}
    <div class="p-3 flex flex-col gap-2 min-h-0">
      <p class="text-xs text-neutral-500">Merged PRs by how they landed.</p>
      <div class="flex gap-1">
        {#each ['squash', 'merge'] as m}
          <button
            class="px-2 py-1 text-xs rounded {method === m ? 'bg-emerald-600 text-white' : 'border border-neutral-300 dark:border-neutral-700'}"
            onclick={() => { method = m; runPRs() }}
          >{m}</button>
        {/each}
      </div>
      {#if prErr}<p class="text-xs text-red-600">{prErr}</p>{/if}
      {#if prs.length}
        <p class="text-xs text-neutral-500">{prs.length} PRs</p>
        <ul class="flex flex-col gap-1 overflow-auto">
          {#each prs as p}
            <li class="text-xs leading-tight">
              <a class="font-mono text-[10px] text-emerald-600" href={p.url} target="_blank" rel="noopener">#{p.num}</a>
              {#if p.headRef}<span class="text-neutral-500">{p.headRef} → {p.baseRef}</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</aside>
