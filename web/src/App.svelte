<script lang="ts">
  import { fetchGraph } from './lib/api'
  import type { Graph } from './lib/types'
  import Landing from './lib/Landing.svelte'
  import Swimlane from './lib/Swimlane.svelte'
  import QueryPanel from './lib/QueryPanel.svelte'
  import Releases from './lib/Releases.svelte'

  let view = $state<'landing' | 'viewer'>('landing')
  let tab = $state<'graph' | 'releases'>('graph')
  let runId = $state('')
  let graph = $state<Graph | null>(null)
  let error = $state('')
  let loading = $state(false)
  let showPanel = $state(true)

  async function openRun(id: string) {
    runId = id
    view = 'viewer'
    tab = 'graph'
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

  function newAnalysis() {
    view = 'landing'
    graph = null
    runId = ''
    error = ''
  }
</script>

{#if view === 'landing'}
  <div class="h-full">
    <Landing onOpen={openRun} />
  </div>
{:else}
  <div class="flex flex-col h-full">
    <header class="flex items-center gap-4 px-4 py-2 border-b border-neutral-200 dark:border-neutral-800 shrink-0">
      <button
        class="text-xs px-2 py-1 rounded border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800"
        onclick={newAnalysis}
        title="Analyze another repository"
      >← New</button>
      <h1 class="text-sm font-semibold tracking-tight">Git Branch Graph</h1>

      <!-- Graph | Releases tabs -->
      {#if graph}
        <nav class="flex items-center rounded-md border border-neutral-300 dark:border-neutral-700 overflow-hidden text-xs">
          <button
            class="px-3 py-1 {tab === 'graph' ? 'bg-emerald-600 text-white' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800'}"
            onclick={() => (tab = 'graph')}
          >Graph</button>
          <button
            class="px-3 py-1 border-l border-neutral-300 dark:border-neutral-700 {tab === 'releases' ? 'bg-emerald-600 text-white' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800'}"
            onclick={() => (tab = 'releases')}
          >Releases</button>
        </nav>
      {/if}

      {#if graph}
        <span class="text-xs text-neutral-500">
          <b class="font-mono text-neutral-700 dark:text-neutral-300">{graph.meta.org}/{graph.meta.repo}</b>
          · {graph.meta.defaultBranch}
        </span>
        <span class="text-xs text-neutral-400 tabular-nums">
          {graph.meta.counts.commits.toLocaleString()} commits · {graph.meta.counts.branches} branches · {graph.meta.counts.tags} tags
        </span>
      {/if}
      {#if graph && tab === 'graph'}
        <button
          class="ml-auto text-xs px-2 py-1 rounded border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800"
          onclick={() => (showPanel = !showPanel)}
        >{showPanel ? 'Hide' : 'Show'} queries</button>
      {/if}
    </header>

    <main class="flex-1 min-h-0 flex">
      {#if error}
        <p class="p-4 text-sm text-red-600">Error: {error}</p>
      {:else if loading}
        <p class="p-4 text-sm text-neutral-500">Loading graph…</p>
      {:else if graph && tab === 'graph'}
        <div class="flex-1 min-w-0">
          <Swimlane {graph} {runId} />
        </div>
        {#if showPanel}
          <QueryPanel {graph} {runId} />
        {/if}
      {:else if graph && tab === 'releases'}
        <div class="flex-1 min-w-0">
          <Releases {graph} {runId} />
        </div>
      {/if}
    </main>
  </div>
{/if}
