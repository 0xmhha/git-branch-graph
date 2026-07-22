<script lang="ts">
  import { fetchRuns } from './api'
  import type { RunSummary } from './types'
  import ProgressModal from './ProgressModal.svelte'

  let { onOpen }: { onOpen: (runId: string) => void } = $props()

  let input = $state('')
  let runs = $state<RunSummary[]>([])
  let jobId = $state('')
  let starting = $state(false)
  let startErr = $state('')

  $effect(() => {
    fetchRuns()
      .then((r) => (runs = r))
      .catch(() => {})
  })

  // Light client-side hint of how the input will be treated.
  const kind = $derived.by(() => {
    const v = input.trim()
    if (!v) return ''
    if (/^https?:\/\//.test(v) || /^git@/.test(v)) return 'Remote repository'
    if (v.includes('/') || v.includes('\\')) return 'Local path'
    return 'Local path'
  })

  async function submit() {
    const v = input.trim()
    if (!v || starting) return
    starting = true
    startErr = ''
    try {
      const r = await fetch('/api/ingest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ input: v }),
      })
      if (!r.ok) throw new Error(await r.text())
      jobId = (await r.json()).jobId
    } catch (e) {
      startErr = String(e)
    } finally {
      starting = false
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter') submit()
  }
</script>

<div class="relative min-h-full grid place-items-center px-6 overflow-hidden">
  <!-- subtle branch-column motif in the far background -->
  <svg class="pointer-events-none absolute inset-0 w-full h-full opacity-[0.05] dark:opacity-[0.07]" aria-hidden="true">
    {#each [0, 1, 2, 3, 4] as i}
      <line x1={`${18 + i * 16}%`} x2={`${18 + i * 16}%`} y1="0" y2="100%" stroke="currentColor" stroke-width="1.5" />
    {/each}
    <line x1="18%" x2="50%" y1="38%" y2="38%" stroke="currentColor" stroke-width="1.5" />
    <line x1="34%" x2="66%" y1="62%" y2="62%" stroke="currentColor" stroke-width="1.5" />
  </svg>

  <div class="relative w-full max-w-xl flex flex-col items-center text-center">
    <div class="flex items-center gap-2.5">
      <svg width="26" height="26" viewBox="0 0 24 24" fill="none" class="text-emerald-500">
        <circle cx="6" cy="5" r="2.4" fill="currentColor" />
        <circle cx="6" cy="19" r="2.4" fill="currentColor" />
        <circle cx="18" cy="12" r="2.4" fill="currentColor" />
        <path d="M6 7.4v9.2M6 12h5.5a4 4 0 004-4V7" stroke="currentColor" stroke-width="1.8" fill="none" />
      </svg>
      <h1 class="text-xl font-semibold tracking-tight">Git Branch Graph</h1>
    </div>
    <p class="mt-2 text-sm text-neutral-500">
      See branches, merges, squashes and releases across any repository — as fixed columns on a timeline.
    </p>

    <!-- the input -->
    <div class="mt-8 w-full">
      <div class="flex items-stretch rounded-lg border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 focus-within:border-emerald-500 focus-within:ring-1 focus-within:ring-emerald-500 transition">
        <input
          class="flex-1 min-w-0 px-3.5 py-2.5 bg-transparent text-sm font-mono placeholder:text-neutral-400 outline-none"
          placeholder="github.com/org/repo, /path/to/repo, or data/<run>"
          bind:value={input}
          onkeydown={onKey}
          spellcheck="false"
          autocapitalize="off"
          autocorrect="off"
        />
        <button
          class="shrink-0 px-4 my-1 mr-1 rounded-md text-sm font-medium bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white transition"
          onclick={submit}
          disabled={!input.trim() || starting}
        >
          {starting ? '…' : 'Analyze'}
        </button>
      </div>
      <div class="mt-2 h-4 flex items-center justify-between text-[11px] text-neutral-400">
        <span>Remote URL · local repo path · already-analyzed folder</span>
        {#if kind}<span class="text-emerald-600 dark:text-emerald-400">{kind}</span>{/if}
      </div>
      {#if startErr}<p class="mt-1 text-xs text-red-600 text-left">{startErr}</p>{/if}
    </div>

    <!-- recent analyses -->
    {#if runs.length}
      <div class="mt-10 w-full">
        <div class="text-[11px] uppercase tracking-wide text-neutral-400 mb-2 text-left">Recent analyses</div>
        <div class="flex flex-col gap-1.5">
          {#each runs.slice(0, 6) as r}
            <button
              class="group flex items-center gap-3 w-full text-left px-3 py-2 rounded-md border border-neutral-200 dark:border-neutral-800 hover:border-emerald-400 dark:hover:border-emerald-600 hover:bg-neutral-50 dark:hover:bg-neutral-900 transition"
              onclick={() => onOpen(r.id)}
            >
              <span class="font-mono text-xs font-medium truncate">{r.org}/{r.repo}</span>
              <span class="text-[11px] text-neutral-400">{r.defaultBranch}</span>
              <span class="ml-auto text-[11px] tabular-nums text-neutral-400">
                {r.commits.toLocaleString()} commits · {r.branches} br
              </span>
              <span class="text-neutral-300 dark:text-neutral-600 group-hover:text-emerald-500 transition">→</span>
            </button>
          {/each}
        </div>
      </div>
    {/if}
  </div>
</div>

{#if jobId}
  <ProgressModal
    {jobId}
    onDone={(runId) => {
      jobId = ''
      onOpen(runId)
    }}
    onClose={() => (jobId = '')}
  />
{/if}
