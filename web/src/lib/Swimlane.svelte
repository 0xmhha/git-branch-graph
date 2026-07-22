<script lang="ts">
  import type { Graph, GraphNode, Containment } from './types'
  import { fetchContainment } from './api'

  let { graph, runId }: { graph: Graph; runId: string } = $props()

  // Fixed per-branch columns (from the backend), X = column, Y = time.
  const rowH = 26
  const colW = 160
  const gutterW = 108
  const nodeR = 5
  const padT = 18
  const buffer = 28

  const columns = $derived(graph.columns)
  const ncols = $derived(columns.length)
  const otherIdx = $derived(columns.findIndex((c) => c.kind === 'other'))

  const indexOf = $derived.by(() => {
    const m = new Map<string, number>()
    graph.nodes.forEach((n, i) => m.set(n.sha, i))
    return m
  })

  // C2 — viewport range (client-side; data is fully loaded, newest-first) + jump.
  type RangeOpt = { label: string; kind: 'count' | 'months' | 'all'; v?: number }
  const rangeOptions: RangeOpt[] = [
    { label: 'recent 200', kind: 'count', v: 200 },
    { label: 'recent 1,000', kind: 'count', v: 1000 },
    { label: 'recent 5,000', kind: 'count', v: 5000 },
    { label: 'last 3 months', kind: 'months', v: 3 },
    { label: 'last 12 months', kind: 'months', v: 12 },
    { label: 'all', kind: 'all' },
  ]
  // Default to "recent 1,000": caps large repos, and shows everything for small
  // ones (Math.min with total), so it works well at any size.
  let rangeIdx = $state(1)

  function computeLimit(idx: number): number {
    const total = graph.nodes.length
    const o = rangeOptions[idx]
    if (o.kind === 'all') return total
    if (o.kind === 'count') return Math.min(total, o.v ?? total)
    const newest = Date.parse(graph.nodes[0]?.committedAt ?? '')
    if (isNaN(newest)) return total
    const cutoff = newest - (o.v ?? 0) * 30.44 * 24 * 3600 * 1000
    let i = 0
    while (i < total && Date.parse(graph.nodes[i].committedAt) >= cutoff) i++
    return Math.max(1, i)
  }
  // N = number of most-recent commits rendered (the viewport window).
  const N = $derived(computeLimit(rangeIdx))
  let jumpText = $state('')
  let jumpFlash = $state(-1)

  const colX = (c: number) => gutterW + c * colW + colW / 2
  const y = (i: number) => padT + i * rowH
  const totalW = $derived(gutterW + ncols * colW)
  const totalH = $derived(padT * 2 + N * rowH)

  // Vertical extent (row range) of each column, within the current window.
  const extent = $derived.by(() => {
    const min = new Array(ncols).fill(Infinity)
    const max = new Array(ncols).fill(-Infinity)
    for (let i = 0; i < N; i++) {
      const c = graph.nodes[i].col
      if (i < min[c]) min[c] = i
      if (i > max[c]) max[c] = i
    }
    return { min, max }
  })

  // Spine geometry per column, applying the drawing rules:
  //  - default / active: line reaches the header (y=0) down to the oldest commit.
  //  - stale:            line spans only its commit extent.
  //  - other:            no spine (unrelated merged-in commits; dots only).
  const spines = $derived.by(() =>
    columns
      .map((col, c) => {
        if (extent.max[c] < extent.min[c]) return null
        if (col.kind === 'other') return null
        const top = col.kind === 'default' || col.kind === 'active' ? 0 : y(extent.min[c])
        return { c, x: colX(c), y1: top, y2: y(extent.max[c]), color: col.color }
      })
      .filter((s): s is { c: number; x: number; y1: number; y2: number; color: string } => s !== null),
  )

  // Edges resolved to rows + columns.
  const edgeRows = $derived.by(() =>
    graph.edges.map((e) => {
      const ci = indexOf.get(e.child) ?? -1
      const pi = indexOf.get(e.parent) ?? -1
      return {
        e,
        ci,
        pi,
        cc: ci >= 0 ? graph.nodes[ci].col : -1,
        pc: pi >= 0 ? graph.nodes[pi].col : -1,
      }
    }),
  )

  // Virtualization.
  let scrollTop = $state(0)
  let scrollLeft = $state(0)
  let viewportH = $state(600)
  let scroller = $state<HTMLDivElement | null>(null)
  const visStart = $derived(Math.max(0, Math.floor(scrollTop / rowH) - buffer))
  const visEnd = $derived(Math.min(N, Math.ceil((scrollTop + viewportH) / rowH) + buffer))
  const visNodes = $derived(graph.nodes.slice(visStart, visEnd))
  // Only cross-column edges are drawn (same-column runs are covered by the spine);
  // both endpoints must fall inside the current window.
  const visEdges = $derived(
    edgeRows.filter(({ ci, pi, cc, pc }) => {
      if (ci < 0 || pi < 0 || cc === pc || ci >= N || pi >= N) return false
      return !(Math.max(ci, pi) < visStart || Math.min(ci, pi) > visEnd)
    }),
  )
  function onScroll() {
    if (!scroller) return
    scrollTop = scroller.scrollTop
    scrollLeft = scroller.scrollLeft
  }
  $effect(() => {
    if (!scroller) return
    const ro = new ResizeObserver(() => {
      if (scroller) viewportH = scroller.clientHeight
    })
    ro.observe(scroller)
    return () => ro.disconnect()
  })

  // Connector: a single horizontal line at the CHILD commit's own row, from the
  // parent branch's spine across to the child. Each fork/merge gets its own line
  // at its own timeline row — lines are never merged together.
  function horizPath(px: number, cx: number, cy: number): string {
    return `M${px} ${cy} L${cx} ${cy}`
  }

  // Horizontal triangle arrowhead at the child, pointing toward it (the travel
  // direction along the connector). Explicit polygon — no SVG marker.
  function arrowHead(cx: number, cy: number, rightward: boolean): string {
    const g = nodeR + 1
    if (rightward) return `${cx - g},${cy} ${cx - g - 6},${cy - 3.5} ${cx - g - 6},${cy + 3.5}`
    return `${cx + g},${cy} ${cx + g + 6},${cy - 3.5} ${cx + g + 6},${cy + 3.5}`
  }

  // Hover + tooltip.
  let hovered = $state<GraphNode | null>(null)
  let mouseX = $state(0)
  let mouseY = $state(0)
  let contain = $state<Containment | null>(null)
  const containCache = new Map<string, Containment>()
  const hoveredIndex = $derived(hovered ? (indexOf.get(hovered.sha) ?? -1) : -1)

  // C1 — branch highlight (click a column header) + commit filter.
  let highlightCol = $state(-1)
  let filter = $state('')
  const filterLc = $derived(filter.trim().toLowerCase())
  function matches(n: GraphNode): boolean {
    if (!filterLc) return true
    const pr = n.prNum ?? ''
    return (
      n.subject.toLowerCase().includes(filterLc) ||
      n.author.toLowerCase().includes(filterLc) ||
      (n.branchOf?.toLowerCase().includes(filterLc) ?? false) ||
      pr === filterLc.replace('#', '') ||
      ('#' + pr).includes(filterLc)
    )
  }
  function dimmed(n: GraphNode): boolean {
    if (highlightCol >= 0 && n.col !== highlightCol) return true
    if (filterLc && !matches(n)) return true
    return false
  }
  function toggleHighlight(c: number) {
    highlightCol = highlightCol === c ? -1 : c
  }
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
        /* optional */
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

  // Header sublabel = GitFlow role, with a staleness hint when applicable.
  function subLabel(col: { role: string; kind: string }): string {
    if (col.role === 'other') return ''
    return col.kind === 'stale' ? `${col.role} · stale` : col.role
  }

  // Jump to a commit by SHA or PR number (declared last so all refs are in scope).
  function jump() {
    const v = jumpText.trim().replace(/^#/, '')
    if (!v) return
    let idx = /^\d+$/.test(v) ? graph.nodes.findIndex((n) => n.prNum === v) : -1
    if (idx < 0) idx = graph.nodes.findIndex((n) => n.sha.startsWith(v.toLowerCase()))
    if (idx < 0) return
    if (idx >= N) rangeIdx = rangeOptions.length - 1 // widen to 'all' so it's visible
    requestAnimationFrame(() => {
      scroller?.scrollTo({ top: Math.max(0, y(idx) - viewportH / 2), behavior: 'smooth' })
    })
    jumpFlash = idx
    setTimeout(() => {
      if (jumpFlash === idx) jumpFlash = -1
    }, 1800)
  }
</script>

<div class="flex flex-col h-full">
  <!-- filter / highlight toolbar -->
  <div class="flex items-center gap-2 px-2.5 py-1.5 border-b border-neutral-200 dark:border-neutral-800 shrink-0">
    <input
      class="w-64 px-2 py-1 rounded-md border border-neutral-300 dark:border-neutral-700 bg-transparent outline-none focus:border-emerald-500 font-mono text-[11px]"
      placeholder="filter commits — subject / author / #PR"
      bind:value={filter}
    />
    {#if highlightCol >= 0}
      <button
        class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-[11px] font-medium"
        style="color:{columns[highlightCol]?.color ?? '#8b949e'}; background:color-mix(in srgb, {columns[highlightCol]?.color ?? '#8b949e'} 15%, transparent)"
        onclick={() => (highlightCol = -1)}
      >highlighting {columns[highlightCol]?.name} ✕</button>
    {/if}
    {#if filter}
      <button class="text-[11px] text-neutral-400 hover:text-neutral-600" onclick={() => (filter = '')}>clear</button>
    {/if}

    <div class="ml-auto flex items-center gap-2">
      <select
        class="px-1.5 py-1 text-[11px] rounded-md border border-neutral-300 dark:border-neutral-700 bg-transparent outline-none"
        bind:value={rangeIdx}
        title="Rendered range"
      >
        {#each rangeOptions as o, i}
          <option value={i}>{o.label}</option>
        {/each}
      </select>
      <span class="text-[11px] text-neutral-400 tabular-nums whitespace-nowrap">{N.toLocaleString()} / {graph.nodes.length.toLocaleString()}</span>
      <input
        class="w-40 px-2 py-1 rounded-md border border-neutral-300 dark:border-neutral-700 bg-transparent outline-none focus:border-emerald-500 font-mono text-[11px]"
        placeholder="jump to SHA / #PR"
        bind:value={jumpText}
        onkeydown={(e) => e.key === 'Enter' && jump()}
      />
    </div>
  </div>

  <!-- branch column headers (horizontally synced with the graph) -->
  <div class="relative overflow-hidden border-b border-neutral-200 dark:border-neutral-800 shrink-0" style="height:42px">
    <div class="absolute top-0 left-0 h-full" style="width:{totalW}px; transform:translateX({-scrollLeft}px)">
      {#each columns as col, c}
        <div class="absolute top-1 -translate-x-1/2 flex flex-col items-center gap-0.5" style="left:{colX(c)}px; max-width:{colW - 8}px">
          {#if col.kind === 'other'}
            <span class="text-[11px] text-neutral-400 italic truncate">{col.name}</span>
          {:else}
            <span class="flex items-center gap-0.5 max-w-full">
              <button
                class="px-2 py-0.5 rounded text-[11px] font-semibold truncate {highlightCol === c ? 'ring-1' : ''}"
                style="color:{col.color}; background:color-mix(in srgb, {col.color} {highlightCol === c ? 24 : 15}%, transparent); {highlightCol === c ? `outline:1px solid ${col.color}` : ''}"
                title={`Highlight ${col.name}`}
                onclick={() => toggleHighlight(c)}
              >{col.name}</button>
              <a
                href={`${graph.linkBase}/tree/${col.name}`}
                target="_blank"
                rel="noopener"
                class="text-[10px] text-neutral-400 hover:text-emerald-500 shrink-0"
                title="Open on GitHub"
              >↗</a>
            </span>
          {/if}
          {#if subLabel(col)}
            <span class="text-[9px] uppercase tracking-wide text-neutral-400">{subLabel(col)}</span>
          {/if}
        </div>
      {/each}
    </div>
  </div>

  <div bind:this={scroller} onscroll={onScroll} class="flex-1 overflow-auto">
    <svg width={totalW} height={totalH} style="display:block" role="application" aria-label="Branch column graph" onmousemove={onMove}>
      <!-- background ruler: faint horizontal guides to align the left gutter
           with commits in far-right columns (every 5th row a touch stronger) -->
      {#each Array(Math.max(0, visEnd - visStart)) as _, k}
        {@const i = visStart + k}
        <line x1="0" x2={totalW} y1={y(i)} y2={y(i)} stroke="currentColor" stroke-width="1" opacity={i % 5 === 0 ? 0.09 : 0.035} />
      {/each}
      {#if hoveredIndex >= 0}
        <rect x="0" y={y(hoveredIndex) - rowH / 2} width={totalW} height={rowH} fill="currentColor" opacity="0.07" />
      {/if}
      {#if jumpFlash >= 0}
        <rect x="0" y={y(jumpFlash) - rowH / 2} width={totalW} height={rowH} fill="#10b981" opacity="0.16" />
      {/if}

      <!-- branch spines (rule-based extent) -->
      {#each spines as s (s.c)}
        <line
          x1={s.x} x2={s.x} y1={s.y1} y2={s.y2}
          stroke={s.color}
          stroke-width={highlightCol === s.c ? 3 : 2}
          opacity={highlightCol < 0 ? 0.28 : highlightCol === s.c ? 0.55 : 0.08}
          stroke-linecap="round"
        />
      {/each}

      <!-- cross-column connectors: one horizontal line per fork/merge at the
           child's row; prominent (with arrowhead) except merged-in commits -->
      {#each visEdges as { e, ci, pi, cc, pc } (e.child + '>' + e.parent)}
        {@const faint = cc === otherIdx || pc === otherIdx}
        {@const stroke = e.parentIndex === 0 ? graph.nodes[ci].color : graph.nodes[pi].color}
        {@const dashed = e.type === 'squash' || e.type === 'cherry'}
        {@const off = highlightCol >= 0 && cc !== highlightCol && pc !== highlightCol}
        {@const op = (faint ? 0.22 : 0.95) * (off ? 0.18 : 1)}
        <path
          d={horizPath(colX(pc), colX(cc), y(ci))}
          fill="none"
          stroke={stroke}
          stroke-width={faint ? 1 : 2}
          stroke-dasharray={dashed ? '4 3' : undefined}
          opacity={op}
        />
        {#if !faint && !off}
          <polygon points={arrowHead(colX(cc), y(ci), colX(cc) > colX(pc))} fill={stroke} />
        {/if}
      {/each}

      <!-- nodes + gutter labels -->
      {#each visNodes as n (n.sha)}
        {@const i = indexOf.get(n.sha) ?? 0}
        {@const cx = colX(n.col)}
        {@const cy = y(i)}
        {@const isOther = n.col === otherIdx}
        {@const dim = dimmed(n)}
        <g role="listitem" onmouseenter={(ev) => onEnter(n, ev)} onmouseleave={onLeave} opacity={dim ? 0.16 : 1}>
          <rect x="0" y={cy - rowH / 2} width={totalW} height={rowH} fill="transparent" />
          {#if n.prNum}
            <text
              x="8" y={cy + 3.5} font-size="10.5" font-family="ui-monospace, monospace" font-weight="600"
              fill={n.prVerified === 'unverified' ? '#d29922' : n.mergeMethod === 'merge' ? '#58a6ff' : '#a371f7'}
            >PR #{n.prNum}{n.prVerified === 'unverified' ? '?' : ''}</text>
          {:else}
            <text x="8" y={cy + 3.5} font-size="10" font-family="ui-monospace, monospace" fill="#8b949e">{n.sha.slice(0, 7)}</text>
          {/if}
          <a href={n.links.commit} target="_blank" rel="noopener">
            {#if n.isMerge}
              <circle {cx} {cy} r={nodeR + 1.5} fill="none" stroke={n.color} stroke-width="1.7" opacity={isOther ? 0.5 : 1} />
              <circle {cx} {cy} r={nodeR - 1.5} fill={n.color} opacity={isOther ? 0.5 : 1} />
            {:else}
              <circle {cx} {cy} r={isOther ? 3 : nodeR} fill={n.color} opacity={isOther ? 0.5 : 1} />
            {/if}
          </a>
          {#if n.cherryFrom || (n.cherryTo && n.cherryTo.length)}
            <text x={cx - nodeR - 11} y={cy + 3.5} font-size="9">🍒</text>
          {/if}
          {#if n.refs}
            {#each n.refs.filter((r) => r.type === 'tag') as tag, k}
              <a href={`${graph.linkBase}/releases/tag/${tag.name}`} target="_blank" rel="noopener">
                <text x={cx + nodeR + 5 + k * 4} y={cy + 3.5} font-size="10" fill="#8b949e" font-family="ui-monospace, monospace">◇ {tag.name}</text>
              </a>
            {/each}
          {/if}
        </g>
      {/each}
    </svg>
  </div>
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
    {#if hovered.cherryFrom}
      <div class="mt-0.5 text-[11px] text-pink-600 dark:text-pink-400">🍒 cherry-pick of <span class="font-mono">{hovered.cherryFrom.slice(0, 9)}</span></div>
    {/if}
    {#if hovered.cherryTo && hovered.cherryTo.length}
      <div class="mt-0.5 text-[11px] text-pink-600 dark:text-pink-400">🍒 cherry-picked to {hovered.cherryTo.length} place{hovered.cherryTo.length > 1 ? 's' : ''}</div>
    {/if}
    <div class="mt-0.5 text-neutral-500 flex items-center gap-1.5 flex-wrap">
      <span>{hovered.author} · {new Date(hovered.committedAt).toLocaleString()}</span>
      {#if hovered.prNum}
        <span>· PR #{hovered.prNum}</span>
        {#if hovered.prVerified === 'unverified'}
          <span class="text-amber-600" title="This #number was not found as a PR in this repo — likely an upstream/fork reference; no link.">unverified</span>
        {/if}
      {/if}
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
  </div>
{/if}
