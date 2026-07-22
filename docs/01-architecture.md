# Git Branch Graph — 아키텍처

## 파이프라인 5단계
각 단계는 **입력 → 처리 → 산출**이 명확한 독립 모듈이다. 앞 단계 산출물이 파일로
떨어지므로 개별 재실행·디버깅·캐시가 가능하다.

```
[1] Acquire   URL          → data/.repos/<org>__<repo>.git (bare, blobless)
[2] Extract   local git    → raw/*.csv (commits, refs, edges, prs 뼈대)
[3] Enrich    GitHub API   → raw/prs.csv, raw/checks.csv (PR/CI/링크 보강)
[4] Ontology  raw/*.csv    → graph.json + graph.sqlite (레인·색·판별·containment)
[5] Serve     data/<run>/  → GUI 렌더 (SVG 스윔레인)
```

---

## [1] Acquire — 저장소 확보
**입력:** GitHub URL (`https://github.com/<org>/<repo>[.git]`)

**처리:**
```bash
# 최초
git clone --bare --filter=blob:none <url> data/.repos/<org>__<repo>.git
# 이후 (증분 갱신)
git -C data/.repos/<org>__<repo>.git fetch --prune origin '+refs/heads/*:refs/heads/*'
```
- `--bare`: 워킹트리 없음 → 체크아웃 비용 0
- `--filter=blob:none`: 파일 blob 미다운로드(그래프에 불필요) → go-wemix 기준 174MB→수 MB
- default 브랜치 식별: `git -C <repo>.git symbolic-ref refs/heads/HEAD` 또는
  GitHub API `repository.defaultBranchRef.name` (로컬 우선, API fallback)

**산출:** `data/.repos/<org>__<repo>.git` (bare mirror, 재사용/캐시 대상)

**멱등성:** default HEAD SHA를 계산 → 이미 `data/<run>/` 폴더가 존재하면 [2]~[4] 스킵.

---

## [2] Extract — 로컬 git 1-pass 수집
**입력:** bare repo

**처리:** 단일 `git log` 스캔으로 그래프 뼈대를 CSV로 직행. (API 대비 압도적 효율,
rate-limit 없음)
```bash
git -C <repo>.git log --all --no-abbrev --date=iso-strict \
  --pretty=format:'%H%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%cI%x1f%D%x1f%s'
#                    sha  parents author email adate cdate refs subject
```
- `%P`(부모 목록): 공백 다수 = 머지 커밋 판별의 근거
- `%D`(refs): 브랜치/태그 decoration → refs.csv 소스
- `%x1f`(0x1F unit separator) 구분자: 커밋 메시지의 콤마/파이프 충돌 회피
- refs 별도 수집: `git for-each-ref --format='%(refname)%x1f%(objectname)%x1f%(objecttype)'`
- 태그 containment(무거우면 [4]로 지연): `git tag --contains <sha>`

**PR 번호 로컬 파싱:** subject의 `(#183)` 패턴을 정규식으로 추출 → prs.csv의 pr_num
초기값. (번호는 API 없이 확보; 병합방식·상태는 [3]에서 보강)

**산출:** `raw/commits.csv`, `raw/refs.csv`, `raw/edges.csv` (스키마: `02-data-model.md`)

---

## [3] Enrich — GitHub API 보강 (git이 못 주는 것만)
**입력:** raw/commits.csv (pr_num 목록), URL의 org/repo

**처리:** **GraphQL 배치**로 최소 호출. REST N-콜 금지.
- PR 메타: `state`, `mergedAt`, `mergeCommit.oid`, **병합방식 추정**, `baseRefName`,
  `headRefName`, `url`
- (옵션) CI/체크: default HEAD 근처 커밋의 `statusCheckRollup`
- default 브랜치명 (로컬로 못 구했을 때 fallback)
- 링크 베이스: `https://github.com/<org>/<repo>` (commit/pull/tree/releases/tag URL 조립)

**병합방식 판별 로직 (git + API 결합):**
| 관측 | 판정 |
|---|---|
| 커밋 부모 2개 + PR mergeCommit == 이 커밋 | `merge` |
| 커밋 부모 1개 + subject에 `(#N)` + PR state=merged + PR head 브랜치 tip과 patch-id 일치 | `squash` |
| 커밋 부모 1개 + 다른 브랜치에 동일 patch-id 존재 | `cherry-pick` |
| 그 외 | `commit` |

**토큰:** gitfut `lib/github/tokens.ts` 패턴 재사용(env 토큰 로테이션·rate-limit 대응).
토큰 없으면 [3] 스킵 → 그래프는 git 정보만으로도 렌더 가능(PR 뱃지·CI만 비활성).

**산출:** `raw/prs.csv`, `raw/checks.csv`

---

## [4] Ontology — 관계 계산 계층
**입력:** raw/*.csv

**처리:** raw(사실) 위에서 렌더에 필요한 **파생 지식**을 계산. (상세: `03-graph-rendering.md`)
- **레인 배정**: 위상 정렬 + greedy 열 재사용 → 각 커밋에 `lane` 정수
- **색 배정**: 브랜치명 → 결정적 색(해시→팔레트), default 브랜치는 고정 primary
- **엣지 타입 확정**: commit/merge/squash/cherry (raw + patch-id)
- **containment**: `git tag/branch --contains` → "이 커밋이 포함된 태그·브랜치"
- **링크 조립**: 각 노드/엣지에 GitHub URL 부착

**두 가지 형태로 산출 (둘 다 1급):**
- `graph.json` — nodes[]+edges[], 렌더 필요한 모든 속성 계산 완료 (GUI fetch 1회)
- `graph.sqlite` — 정규화 테이블 + 인덱스, 역질의용("이 SHA 어디 들어갔나" 등)

**산출:** `graph.json`, `graph.sqlite`, `meta.json`

---

## [5] Serve — GUI (Svelte SPA + `gbg serve` Go)
**입력:** `data/<run>/` 폴더

**백엔드 `gbg serve` (Go, 서블릿 역할):**
- `GET /api/runs` — `data/` 폴더 목록(meta 요약)
- `GET /api/runs/:id/graph.json` — 렌더 데이터 서빙
- `GET /api/runs/:id/query?...` — `graph.sqlite` **서버사이드 질의** → 작은 JSON
  (브라우저는 239MB DB를 로드하지 않음)
- `POST /api/ingest?url=...` — [1]~[4] 잡 트리거(선택)
- `/` — `web/dist`를 `embed.FS`로 정적 호스팅

**프론트 Svelte SPA:**
- `graph.json` fetch → **SVG 스윔레인** 렌더 (Y=노드 인덱스, X=lane; 색·링크 선계산)
- 호버 툴팁 + GitHub 하이퍼링크
- 역질의/필터는 `/api/query`로 on-demand (sql.js는 순수정적 배포 폴백)
- 개발: Vite dev(5173) → `/api` 프록시 → Go(8080); 배포: 단일 바이너리

**"실시간성"의 정의:** GUI는 항상 "마지막 ingest 스냅샷"을 본다. URL 제출/갱신 버튼이
[1](fetch 증분)~[4]를 트리거하는 **on-demand 스냅샷** 모델. HEAD 불변이면 캐시 폴더 재사용.

---

## 디렉토리 구조 (프로젝트)
코어는 Go(표준 레이아웃), 웹은 별도 `web/`(Node). 둘은 `data/`로만 만난다.
```
git-branch-graph/
├─ cmd/gbg/           # Go CLI 진입점 (ingest / ontology / serve)
├─ internal/
│  ├─ acquire/        # [1] clone/fetch (bare, blobless)
│  ├─ extract/        # [2] git 1-pass → raw csv
│  ├─ enrich/         # [3] GitHub GraphQL 보강
│  ├─ ontology/       # [4] 레인·색·판별·containment → json
│  ├─ db/             # [4] sqlite(modernc) 생성
│  ├─ loader/         # raw/*.csv 재로드 (standalone ontology)
│  ├─ serve/          # [5] gbg serve: HTTP API + 정적 호스팅
│  ├─ model/          # 공통 타입(Commit, Ref, Edge, Node, GEdge ...)
│  ├─ gitcmd/         # git 서브프로세스 헬퍼
│  ├─ csvw/           # RFC4180 CSV writer
│  └─ paths/          # 슬러그·content-address 폴더 네이밍
├─ web/               # [5] Svelte SPA (Vite) — SVG 스윔레인, dist를 Go에 embed
├─ data/
│  ├─ .repos/                      # bare mirror 캐시 (gitignore)
│  └─ <org>__<repo>__<branch>__<sha7>/
│     ├─ raw/  (commits.csv, refs.csv, edges.csv, prs.csv, checks.csv)
│     ├─ graph.json
│     ├─ graph.sqlite
│     └─ meta.json
├─ go.mod
└─ docs/  (설계 문서 사본)
```

## 모듈 경계 원칙
- 각 단계는 **파일 경계**로 분리 → 단계별 단독 실행/재실행 가능
- [1][2][4]는 로컬만으로 완결(오프라인 가능), [3]만 네트워크/토큰 의존 → **[3] 없이도
  그래프가 렌더**되도록 degrade
- 계산은 [4]에 집중, GUI는 "이미 계산된 것을 그린다"만 (렌더 로직에 분석 섞지 않음)
