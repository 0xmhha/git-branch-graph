# Git Branch Graph — 데이터 모델

3계층: **raw CSV(사실)** → **온톨로지(파생 관계)** → **SQLite/JSON(질의·렌더용)**.
SQL은 처음부터 1급 산출물이다.

---

## 계층 1: raw CSV (사실 그대로)
속성그래프 스키마로 처음부터 나눈다 → 온톨로지 계층이 재파싱이 아니라 조인이 된다.
구분자는 `,`가 아니라 **0x1F(unit separator)** 권장(커밋 메시지 충돌 회피). 헤더 포함.

### `raw/commits.csv` — 노드(커밋)
| 컬럼 | 예시 | 출처 |
|---|---|---|
| `sha` | `11eb943df...` | git `%H` |
| `parents` | `724cfa1a4 07f386314` | git `%P` (공백구분, 2+면 머지) |
| `author_name` | `alice` | git `%an` |
| `author_email` | `a@x.io` | git `%ae` |
| `authored_at` | `2026-07-10T09:00:00+09:00` | git `%aI` |
| `committed_at` | `2026-07-10T09:05:00+09:00` | git `%cI` |
| `refs` | `HEAD -> release/w0.10.14, tag: ...` | git `%D` |
| `subject` | `fix: prevent stale work (#186)` | git `%s` |
| `pr_num` | `186` | subject `(#N)` 로컬 파싱 |
| `is_merge` | `0/1` | parents 개수 |

### `raw/refs.csv` — 브랜치/태그 참조
| 컬럼 | 예시 | 출처 |
|---|---|---|
| `ref_name` | `release/w0.10.14` | for-each-ref `%(refname:short)` |
| `ref_type` | `branch` \| `tag` | `%(objecttype)`/refname 판정 |
| `target_sha` | `11eb943df...` | `%(objectname)` (태그는 peel) |
| `is_default` | `0/1` | default 브랜치 여부 |

### `raw/edges.csv` — 부모 관계(위상 엣지)
| 컬럼 | 예시 | 의미 |
|---|---|---|
| `child_sha` | `11eb943df` | 자식 |
| `parent_sha` | `724cfa1a4` | 부모 |
| `parent_index` | `0` | 0=first-parent(주 라인), 1+=머지 유입 |
| `edge_type` | `commit`\|`merge`\|`squash`\|`cherry` | [4]에서 확정(초기 commit/merge) |

### `raw/prs.csv` — PR 메타 (API 유래, [3])
| 컬럼 | 예시 |
|---|---|
| `pr_num` | `186` |
| `state` | `merged`\|`open`\|`closed` |
| `merge_method` | `squash`\|`merge`\|`rebase`\|`unknown` |
| `merge_sha` | `11eb943df` |
| `base_ref` | `dev` |
| `head_ref` | `fix/sync-check-work-regression` |
| `url` | `https://github.com/<org>/<repo>/pull/186` |

### `raw/checks.csv` — CI/체크 (옵션, [3])
| 컬럼 | 예시 |
|---|---|
| `sha` | `11eb943df` |
| `context` | `build` |
| `state` | `success`\|`failure`\|`pending` |
| `url` | `https://.../checks/...` |

---

## 계층 2: 온톨로지 (파생 관계 계산)
raw 위에서 계산하는 "지식". 결과는 계층 3에 실린다.
- `lane`(int): 커밋별 위상 레인 열
- `color`(hex): 브랜치 소속 색(default=고정 primary, 그 외 해시→팔레트)
- `branch_of`(str): 커밋이 위치한 대표 브랜치(first-parent 귀속)
- `edge_type` 확정: patch-id 대조로 squash/cherry 판정
- `contained_in_tags` / `contained_in_branches`: `git tag/branch --contains`
- `links`: 노드별 `{commit, pr, tree, tag}` URL

---

## 계층 3: SQLite 스키마 (역질의·질의 1급)
단일 파일 `graph.sqlite`. 서버 불필요, 브라우저(sql.js WASM)에서도 로드 가능.

```sql
-- 노드
CREATE TABLE commits (
  sha           TEXT PRIMARY KEY,
  author_name   TEXT,
  author_email  TEXT,
  authored_at   TEXT,          -- ISO8601
  committed_at  TEXT,
  subject       TEXT,
  pr_num        INTEGER,        -- nullable
  is_merge      INTEGER NOT NULL DEFAULT 0,
  lane          INTEGER,        -- 온톨로지 계산
  color         TEXT,           -- '#rrggbb'
  branch_of     TEXT            -- first-parent 귀속 브랜치
);

-- 위상 엣지 (부모 관계)
CREATE TABLE edges (
  child_sha    TEXT NOT NULL,
  parent_sha   TEXT NOT NULL,
  parent_index INTEGER NOT NULL,
  edge_type    TEXT NOT NULL,   -- commit|merge|squash|cherry
  PRIMARY KEY (child_sha, parent_sha),
  FOREIGN KEY (child_sha)  REFERENCES commits(sha),
  FOREIGN KEY (parent_sha) REFERENCES commits(sha)
);

-- 참조 (브랜치/태그)
CREATE TABLE refs (
  ref_name   TEXT PRIMARY KEY,
  ref_type   TEXT NOT NULL,     -- branch|tag
  target_sha TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (target_sha) REFERENCES commits(sha)
);

-- PR
CREATE TABLE prs (
  pr_num       INTEGER PRIMARY KEY,
  state        TEXT,
  merge_method TEXT,
  merge_sha    TEXT,
  base_ref     TEXT,
  head_ref     TEXT,
  url          TEXT
);

-- CI 체크 (옵션)
CREATE TABLE checks (
  sha     TEXT NOT NULL,
  context TEXT NOT NULL,
  state   TEXT,
  url     TEXT,
  PRIMARY KEY (sha, context),
  FOREIGN KEY (sha) REFERENCES commits(sha)
);

-- containment (커밋 → 포함 태그/브랜치). 역질의 핵심.
CREATE TABLE containment (
  sha       TEXT NOT NULL,
  ref_name  TEXT NOT NULL,      -- 이 커밋을 포함하는 태그/브랜치
  ref_type  TEXT NOT NULL,      -- tag|branch
  PRIMARY KEY (sha, ref_name),
  FOREIGN KEY (sha) REFERENCES commits(sha)
);

-- 실행 메타 (1행)
CREATE TABLE meta (
  repo_url       TEXT,
  org            TEXT,
  repo           TEXT,
  default_branch TEXT,
  head_sha       TEXT,
  captured_at    TEXT,
  commit_count   INTEGER,
  branch_count   INTEGER,
  tag_count      INTEGER
);

CREATE INDEX idx_edges_parent  ON edges(parent_sha);
CREATE INDEX idx_edges_child   ON edges(child_sha);
CREATE INDEX idx_commits_pr    ON commits(pr_num);
CREATE INDEX idx_commits_time  ON commits(committed_at);
CREATE INDEX idx_contain_ref   ON containment(ref_name);
CREATE INDEX idx_contain_sha   ON containment(sha);
```

### 대표 역질의 (SQL이 처음부터 필요한 이유)
```sql
-- "이 수정(SHA/PR)이 어느 태그·브랜치에 포함됐나?"
SELECT ref_name, ref_type FROM containment WHERE sha = :sha;

-- "release/w0.10.14 에 아직 안 들어온 dev 커밋?" (containment 차집합)
SELECT c.sha, c.subject FROM commits c
WHERE EXISTS (SELECT 1 FROM containment WHERE sha=c.sha AND ref_name='dev')
  AND NOT EXISTS (SELECT 1 FROM containment WHERE sha=c.sha AND ref_name='release/w0.10.14');

-- "스쿼시로 들어온 PR 목록"
SELECT pr_num, head_ref, merge_sha FROM prs WHERE merge_method='squash';

-- "태그 w0.10.13 에 포함된 PR"
SELECT DISTINCT c.pr_num FROM commits c
JOIN containment ct ON ct.sha=c.sha
WHERE ct.ref_name='w0.10.13' AND c.pr_num IS NOT NULL;
```

---

## 계층 3: graph.json (GUI 직접 로드용)
SQLite와 **같은 사실의 렌더 최적화 뷰**. GUI가 fetch 1회로 그린다.
```jsonc
{
  "meta": { "org": "...", "repo": "...", "defaultBranch": "dev",
            "headSha": "eccc975c8", "capturedAt": "2026-07-21T17:00:00+09:00",
            "counts": { "commits": 14532, "branches": 8, "tags": 22 } },
  "linkBase": "https://github.com/<org>/<repo>",
  "nodes": [
    { "sha": "11eb943df", "lane": 0, "color": "#39d353",
      "subject": "chore: bump version to v0.10.14",
      "author": "alice", "committedAt": "2026-07-10T09:05:00+09:00",
      "prNum": null, "isMerge": false, "branchOf": "release/w0.10.14",
      "refs": [{ "name": "release/w0.10.14", "type": "branch" }],
      "containedIn": { "tags": [], "branches": ["release/w0.10.14"] },
      "links": { "commit": ".../commit/11eb943df", "pr": null,
                 "tree": ".../tree/release/w0.10.14" } }
  ],
  "edges": [
    { "child": "11eb943df", "parent": "724cfa1a4",
      "parentIndex": 0, "type": "commit", "fromLane": 0, "toLane": 0 },
    { "child": "eccc975c8", "parent": "8380d62f4",
      "parentIndex": 1, "type": "squash", "fromLane": 2, "toLane": 0 }
  ]
}
```

**JSON ↔ SQLite 관계:** 동일 온톨로지 계산 결과의 두 출력. JSON=렌더 편의(레인/색/링크
인라인), SQLite=질의 편의(정규화·인덱스). [4]가 둘을 함께 생성하므로 항상 일관.
