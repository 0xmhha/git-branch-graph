// Mirrors the Go graph.json / API shapes.

export interface RunSummary {
  id: string
  org: string
  repo: string
  defaultBranch: string
  headSha: string
  capturedAt: string
  commits: number
  branches: number
  tags: number
}

export interface NodeRef {
  name: string
  type: 'branch' | 'tag'
}

export interface NodeLinks {
  commit: string
  pr?: string
  tree?: string
}

export interface Column {
  name: string
  kind: 'default' | 'active' | 'stale' | 'other'
  role: 'feature' | 'default' | 'release' | 'hotfix' | 'master' | 'other'
  color: string
  localOnly?: boolean // branch exists only locally — no remote tree to link to
}

export interface GraphNode {
  sha: string
  lane: number
  col: number
  color: string
  subject: string
  author: string
  committedAt: string
  prNum?: string
  isMerge: boolean
  mergeMethod?: string
  ciState?: string
  prVerified?: 'verified' | 'unverified'
  cherryFrom?: string
  cherryTo?: string[]
  branchOf?: string
  refs?: NodeRef[]
  containedBranches?: string[]
  unpushed?: boolean
  links: NodeLinks
}

export interface GraphEdge {
  child: string
  parent: string
  parentIndex: number
  type: 'commit' | 'merge' | 'squash' | 'cherry'
  fromLane: number
  toLane: number
}

export interface Graph {
  meta: {
    org: string
    repo: string
    defaultBranch: string
    headSha: string
    capturedAt: string
    counts: { commits: number; branches: number; tags: number }
  }
  linkBase: string
  columns: Column[]
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface Containment {
  sha: string
  branches: NodeRef[] | null
  tags: NodeRef[] | null
}
