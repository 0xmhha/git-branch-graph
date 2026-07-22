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

export interface GraphNode {
  sha: string
  lane: number
  color: string
  subject: string
  author: string
  committedAt: string
  prNum?: string
  isMerge: boolean
  branchOf?: string
  refs?: NodeRef[]
  containedBranches?: string[]
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
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface Containment {
  sha: string
  branches: NodeRef[] | null
  tags: NodeRef[] | null
}
