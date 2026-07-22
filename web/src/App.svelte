<script lang="ts">
  import { fetchRuns, fetchGraph } from './lib/api'
  import type { RunSummary, Graph } from './lib/types'
  import Swimlane from './lib/Swimlane.svelte'
  import QueryPanel from './lib/QueryPanel.svelte'

  let runs = $state<RunSummary[]>([])
  let selectedId = $state<string>('')
  let graph = $state<Graph | null>(null)
  let error = $state<string>('')
  let loading = $state(false)
  let showPanel = $state(true)

  async function loadRuns() {
    try {
      runs = await fetchRuns()
      if (runs.length && !selectedId) selectRun(runs[0].id)
    } catch (e) {
      error = String(e)
    }
  }

  async function selectRun(id: string) {
    selectedId = id
    loading = true
    graph = null
    error = ''
    try {
      graph = await fetchGraph(id)
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    loadRuns()
  })
</script>

<div class="flex flex-col h-full">
  <header class="flex items-center gap-4 px-4 py-2 border-b border-neutral-200 dark:border-neutral-800">
    <h1 class="text-sm font-semibold tracking-tight">Git Branch Graph</h1>
    <select
      class="text-sm rounded border border-neutral-300 dark:border-neutral-700 bg-transparent px-2 py-1"
      bind:value={selectedId}
      onchange={(e) => selectRun((e.target as HTMLSelectElement).value)}
    >
      {#each runs as r}
        <option value={r.id}>{r.org}/{r.repo} · {r.defaultBranch} · {r.headSha.slice(0, 7)}</option>
      {/each}
    </select>
    {#if graph}
      <span class="text-xs text-neutral-500">
        {graph.meta.counts.commits.toLocaleString()} commits ·
        {graph.meta.counts.branches} branches ·
        {graph.meta.counts.tags} tags
      </span>
    {/if}
    <button
      class="ml-auto text-xs px-2 py-1 rounded border border-neutral-300 dark:border-neutral-700"
      onclick={() => (showPanel = !showPanel)}
    >{showPanel ? 'Hide' : 'Show'} queries</button>
    <span class="text-xs text-neutral-400">
      {#if graph}captured {new Date(graph.meta.capturedAt).toLocaleString()}{/if}
    </span>
  </header>

  <main class="flex-1 min-h-0 flex">
    <div class="flex-1 min-w-0">
      {#if error}
        <p class="p-4 text-sm text-red-600">Error: {error}</p>
      {:else if loading}
        <p class="p-4 text-sm text-neutral-500">Loading graph…</p>
      {:else if graph}
        <Swimlane {graph} runId={selectedId} />
      {:else}
        <p class="p-4 text-sm text-neutral-500">No run selected.</p>
      {/if}
    </div>
    {#if graph && showPanel}
      <QueryPanel {graph} runId={selectedId} />
    {/if}
  </main>
</div>
