<script lang="ts">
  import type { Graph, GraphNode, Containment } from './types'
  import { fetchContainment } from './api'

  let { graph, runId }: { graph: Graph; runId: string } = $props()

  // Layout constants.
  const rowH = 24
  const laneW = 16
  const padL = 20
  const padT = 14
  const nodeR = 4.5
  const buffer = 24 // extra rows rendered above/below the viewport

  // Index maps + geometry (recomputed when graph changes).
  const indexOf = $derived.by(() => {
    const m = new Map<string, number>()
    graph.nodes.forEach((n, i) => m.set(n.sha, i))
    return m
  })
  const maxLane = $derived(graph.nodes.reduce((mx, n) => Math.max(mx, n.lane), 0))
  const labelX = $derived(padL + (maxLane + 1) * laneW + 12)
  const totalH = $derived(padT * 2 + graph.nodes.length * rowH)

  // Precompute edge row indices once per graph.
  const edgeRows = $derived.by(() =>
    graph.edges.map((e) => ({
      e,
      ci: indexOf.get(e.child) ?? -1,
      pi: indexOf.get(e.parent) ?? -1,
    })),
  )

  const x = (lane: number) => padL + lane * laneW
  const y = (idx: number) => padT + idx * rowH

  // Virtualization state.
  let scrollTop = $state(0)
  let viewportH = $state(600)
  let scroller = $state<HTMLDivElement | null>(null)

  const visStart = $derived(Math.max(0, Math.floor(scrollTop / rowH) - buffer))
  const visEnd = $derived(
    Math.min(graph.nodes.length, Math.ceil((scrollTop + viewportH) / rowH) + buffer),
  )
  const visNodes = $derived(graph.nodes.slice(visStart, visEnd))
  const visEdges = $derived(
    edgeRows.filter(({ ci, pi }) => {
      if (ci < 0 || pi < 0) return false
      const lo = Math.min(ci, pi)
      const hi = Math.max(ci, pi)
      return !(hi < visStart || lo > visEnd)
    }),
  )

  function onScroll() {
    if (scroller) scrollTop = scroller.scrollTop
  }
  $effect(() => {
    if (!scroller) return
    const ro = new ResizeObserver(() => {
      if (scroller) viewportH = scroller.clientHeight
    })
    ro.observe(scroller)
    return () => ro.disconnect()
  })

  // Edge path: straight when same lane, gentle S-curve across lanes.
  function edgePath(x1: number, y1: number, x2: number, y2: number): string {
    if (x1 === x2) return `M${x1} ${y1} L${x2} ${y2}`
    const my = (y1 + y2) / 2
    return `M${x1} ${y1} C${x1} ${my},${x2} ${my},${x2} ${y2}`
  }
  const colorOf = (sha: string) => graph.nodes[indexOf.get(sha) ?? 0]?.color ?? '#8b949e'

  // Hover + tooltip.
  let hovered = $state<GraphNode | null>(null)
  let mouseX = $state(0)
  let mouseY = $state(0)
  let contain = $state<Containment | null>(null)
  const containCache = new Map<string, Containment>()

  async function onEnter(n: GraphNode, ev: MouseEvent) {
    hovered = n
    mouseX = ev.clientX
    mouseY = ev.clientY
    contain = containCache.get(n.sha) ?? null
    if (!contain) {
      try {
        const c = await fetchContainment(runId, n.sha)
        containCache.set(n.sha, c)
        if (hovered?.sha === n.sha) contain = c
      } catch {
        /* server-side query optional */
      }
    }
  }
  function onMove(ev: MouseEvent) {
    mouseX = ev.clientX
    mouseY = ev.clientY
  }
  function onLeave() {
    hovered = null
    contain = null
  }
</script>

<div class="relative h-full">
  <div
    bind:this={scroller}
    onscroll={onScroll}
    class="h-full overflow-auto"
  >
    <svg width="100%" height={totalH} style="display:block" role="application" aria-label="Commit swimlane graph" onmousemove={onMove}>
      <!-- edges under nodes -->
      <g fill="none">
        {#each visEdges as { e, ci, pi } (e.child + '>' + e.parent)}
          {@const isFirst = e.parentIndex === 0}
          {@const stroke = isFirst ? colorOf(e.child) : colorOf(e.parent)}
          {@const dashed = e.type === 'squash' || e.type === 'cherry'}
          <path
            d={edgePath(x(graph.nodes[ci].lane), y(ci), x(graph.nodes[pi].lane), y(pi))}
            stroke={stroke}
            stroke-width={isFirst ? 1.75 : 1.5}
            stroke-dasharray={dashed ? '3 3' : undefined}
            opacity="0.9"
          />
        {/each}
      </g>

      <!-- nodes -->
      {#each visNodes as n (n.sha)}
        {@const i = indexOf.get(n.sha) ?? 0}
        {@const cx = x(n.lane)}
        {@const cy = y(i)}
        <a href={n.links.commit} target="_blank" rel="noopener">
          <g
            role="listitem"
            onmouseenter={(ev) => onEnter(n, ev)}
            onmouseleave={onLeave}
          >
            <!-- invisible wide hit area for the whole row -->
            <rect x="0" y={cy - rowH / 2} width="100%" height={rowH} fill="transparent" />
            {#if n.isMerge}
              <circle {cx} {cy} r={nodeR + 1.5} fill="none" stroke={n.color} stroke-width="1.6" />
              <circle {cx} {cy} r={nodeR - 1} fill={n.color} />
            {:else}
              <circle {cx} {cy} r={nodeR} fill={n.color} />
            {/if}

            <!-- ref labels (branch/tag) -->
            {#if n.refs}
              {#each n.refs as ref, k}
                {#if ref.type === 'tag'}
                  <text x={labelX + k * 4} y={cy + 3.5} font-size="10" fill="#8b949e">◇ {ref.name}</text>
                {:else}
                  <text x={labelX + k * 4} y={cy + 3.5} font-size="10" font-weight="600" fill={n.color}>⬤ {ref.name}</text>
                {/if}
              {/each}
            {/if}
          </g>
        </a>
      {/each}
    </svg>
  </div>

  {#if hovered}
    <div
      class="fixed z-10 pointer-events-none max-w-sm rounded-md border border-neutral-300 dark:border-neutral-700 bg-white/95 dark:bg-neutral-900/95 shadow-lg px-3 py-2 text-xs"
      style="left:{Math.min(mouseX + 14, window.innerWidth - 340)}px; top:{mouseY + 14}px"
    >
      <div class="flex items-center gap-2">
        <span class="inline-block w-2 h-2 rounded-full" style="background:{hovered.color}"></span>
        <span class="font-mono text-[11px] text-neutral-500">{hovered.sha.slice(0, 9)}</span>
        {#if hovered.branchOf}<span class="text-neutral-400">· {hovered.branchOf}</span>{/if}
        {#if hovered.isMerge}<span class="text-neutral-400">· merge</span>{/if}
      </div>
      <div class="mt-1 font-medium">{hovered.subject}</div>
      <div class="mt-0.5 text-neutral-500 flex items-center gap-1.5 flex-wrap">
        <span>{hovered.author} · {new Date(hovered.committedAt).toLocaleString()}</span>
        {#if hovered.prNum}<span>· PR #{hovered.prNum}</span>{/if}
        {#if hovered.mergeMethod}
          <span
            class="px-1 rounded text-[10px] font-semibold"
            class:bg-purple-100={hovered.mergeMethod === 'squash'}
            class:text-purple-700={hovered.mergeMethod === 'squash'}
            class:bg-blue-100={hovered.mergeMethod === 'merge'}
            class:text-blue-700={hovered.mergeMethod === 'merge'}
          >{hovered.mergeMethod}</span>
        {/if}
        {#if hovered.ciState}
          <span
            class="px-1 rounded text-[10px] font-semibold"
            class:bg-green-100={hovered.ciState === 'SUCCESS'}
            class:text-green-700={hovered.ciState === 'SUCCESS'}
            class:bg-red-100={hovered.ciState === 'FAILURE'}
            class:text-red-700={hovered.ciState === 'FAILURE'}
            class:bg-amber-100={hovered.ciState !== 'SUCCESS' && hovered.ciState !== 'FAILURE'}
            class:text-amber-700={hovered.ciState !== 'SUCCESS' && hovered.ciState !== 'FAILURE'}
          >CI {hovered.ciState.toLowerCase()}</span>
        {/if}
      </div>
      <div class="mt-1 text-neutral-500">
        <span class="text-neutral-400">tags:</span>
        {#if contain === null}<span class="italic">…</span>
        {:else if contain.tags && contain.tags.length}
          {contain.tags.map((t) => t.name).slice(0, 8).join(', ')}{contain.tags.length > 8 ? ' …' : ''}
        {:else}<span class="italic">none</span>{/if}
      </div>
    </div>
  {/if}
</div>
