<script lang="ts">
  let { jobId, onDone, onClose }: { jobId: string; onDone: (runId: string) => void; onClose: () => void } =
    $props()

  let pct = $state(0)
  let creep = $state(0) // eases the bar forward between real events (liveness)
  let msg = $state('Starting…')
  let errMsg = $state('')

  // Steps light up by progress threshold, independent of exact stage names.
  const STEPS = [
    { label: 'Clone / read repository', doneAt: 40 },
    { label: 'Extract commits & branches', doneAt: 62 },
    { label: 'Fetch PR & CI metadata', doneAt: 85 },
    { label: 'Compute branch graph', doneAt: 100 },
  ]

  // Creep the bar toward (but not past) the next milestone so a long clone/
  // extract still looks alive when no event has arrived for a while.
  $effect(() => {
    const id = setInterval(() => {
      if (errMsg || pct >= 100) return
      const next = STEPS.find((s) => creep < s.doneAt)
      const cap = next ? next.doneAt - 2 : 98
      if (creep < cap) creep = Math.min(cap, creep + 0.5)
    }, 280)
    return () => clearInterval(id)
  })
  const steps = $derived(
    STEPS.map((s, i) => {
      const prevDone = i === 0 ? true : pct >= STEPS[i - 1].doneAt
      const done = pct >= s.doneAt
      return { ...s, status: done ? 'done' : prevDone ? 'active' : 'pending' }
    }),
  )

  $effect(() => {
    const es = new EventSource(`/api/ingest/${jobId}/events`)
    es.onmessage = (e) => {
      const ev = JSON.parse(e.data)
      if (typeof ev.pct === 'number') {
        pct = ev.pct
        if (ev.pct > creep) creep = ev.pct
      }
      if (ev.msg) msg = ev.msg
      if (ev.error) {
        errMsg = ev.error
        es.close()
        return
      }
      if (ev.done && ev.runId) {
        pct = 100
        es.close()
        // brief beat at 100% so the bar visibly completes
        setTimeout(() => onDone(ev.runId), 350)
      }
    }
    es.onerror = () => {
      if (!errMsg && pct < 100) errMsg = 'Connection lost. Is the server still running?'
      es.close()
    }
    return () => es.close()
  })
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
  <div class="w-full max-w-md rounded-xl border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-900 shadow-2xl overflow-hidden">
    <div class="px-5 pt-5 pb-4">
      <div class="flex items-baseline justify-between">
        <h2 class="text-sm font-semibold">{errMsg ? 'Analysis failed' : 'Analyzing repository'}</h2>
        <span class="text-xs tabular-nums text-neutral-400">{Math.round(errMsg ? 100 : Math.max(pct, creep))}%</span>
      </div>

      <!-- progress bar -->
      <div class="mt-3 h-1.5 rounded-full bg-neutral-200 dark:bg-neutral-800 overflow-hidden">
        <div
          class="h-full rounded-full transition-[width] duration-300 ease-out {errMsg ? 'bg-red-500' : 'bg-emerald-500'}"
          style="width:{errMsg ? 100 : Math.max(pct, creep)}%"
        ></div>
      </div>

      <p class="mt-3 text-xs text-neutral-500 min-h-[1rem]">{errMsg || msg}</p>
    </div>

    {#if !errMsg}
      <ul class="px-5 pb-5 flex flex-col gap-2">
        {#each steps as s}
          <li class="flex items-center gap-2.5 text-xs">
            {#if s.status === 'done'}
              <span class="w-4 h-4 rounded-full bg-emerald-500 text-white grid place-items-center text-[10px]">✓</span>
              <span class="text-neutral-500">{s.label}</span>
            {:else if s.status === 'active'}
              <span class="w-4 h-4 rounded-full border-2 border-emerald-500 border-t-transparent animate-spin"></span>
              <span class="font-medium">{s.label}</span>
            {:else}
              <span class="w-4 h-4 rounded-full border border-neutral-300 dark:border-neutral-700"></span>
              <span class="text-neutral-400">{s.label}</span>
            {/if}
          </li>
        {/each}
      </ul>
    {:else}
      <div class="px-5 pb-5">
        <button class="text-xs px-3 py-1.5 rounded-md border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800" onclick={onClose}>
          Back
        </button>
      </div>
    {/if}
  </div>
</div>
