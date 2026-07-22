# Git Branch Graph — 프로젝트 계획

## 위치
- 문서: `study/docs/projects/git-branch-graph/`
- 코드: `new-project/git-branch-graph/` (git init 완료, `main` 브랜치)

## 기술 스택
**코어 로직은 Go, 프론트 대시보드는 Node(Next.js).** 코어(clone·수집·온톨로지·DB 생성)는
단일 정적 바이너리로 배포되는 Go CLI(`gbg`)가 담당하고, 웹은 그 산출물(`data/`)을 읽어
렌더한다. 두 영역은 `data/<run>/` 파일 경계로만 만난다.

| 영역 | 선택 | 근거 |
|---|---|---|
| **코어 CLI** | **Go** (`gbg` 바이너리) | go-wemix와 동일 언어, git 서브프로세스·동시성·단일 바이너리·성능 |
| git 접근 | `git` CLI 서브프로세스 (os/exec) | bare/partial clone, 1-pass log |
| GitHub 수집 | Go GraphQL 클라이언트 (net/http) | 배치 호출, rate-limit 관리 |
| 저장 | CSV(raw) + JSON(렌더) + SQLite(질의) | 계층 분리, SQL 1급 |
| SQLite (생성) | `modernc.org/sqlite` (순수 Go, CGO 불필요) | 크로스컴파일·정적 바이너리 유지 |
| **웹 백엔드** | **`gbg serve` (Go)** | 서블릿 역할을 Go가 담당 — 정적 호스팅 + graph.json 서빙 + SQLite **서버사이드 질의**. 백엔드 통일, 단일 바이너리 |
| **프론트** | **Svelte SPA (Vite 빌드)** | 컴파일러 방식·VDOM 없음 → SVG 다수 렌더에 유리, dataviz 적합. `dist/`를 Go `embed.FS`로 내장 |
| 스타일 | **Tailwind + 인라인 SVG** | 크롬은 Tailwind, 그래프 마크는 SVG 인라인 속성(색은 graph.json에 선계산) |
| SQLite 질의 | **Go 서버사이드**(`/api/query`) | 브라우저가 239MB DB를 로드하지 않음 → 작은 JSON만 수신. sql.js는 순수정적 배포용 폴백 |

### 코어(Go) ↔ 웹(Svelte) 경계
```
[Go 코어 gbg]
  acquire → extract → enrich → ontology → data/<run>/{graph.json, graph.sqlite}
                                                    │
  gbg serve (Go HTTP) ──────────────────────────────┤ 읽음
    ├ GET  /api/runs                → data/ 폴더 목록
    ├ GET  /api/runs/:id/graph.json → 렌더 데이터 서빙
    ├ GET  /api/runs/:id/query?...  → graph.sqlite 서버사이드 질의 → 작은 JSON
    ├ POST /api/ingest?url=...      → gbg ingest 트리거(선택)
    └ /  (embed.FS)                 → Svelte 빌드 정적 호스팅
                                                    ▲ fetch
[Svelte SPA]  브라우저에서 SVG 스윔레인 렌더 + /api 호출 (호버·필터·역질의)
```
- 개발: Vite dev server(5173) → `/api`를 Go(8080)로 프록시
- 배포: `web/dist`를 Go `embed.FS`에 내장 → `gbg serve` 단일 바이너리

## 마일스톤

### M1 — Extract 파이프라인 (로컬만, 네트워크 0) — ✅ 완료 (2026-07-21)
- [x] [1] bare + blobless clone / fetch 증분 (`internal/acquire`)
- [x] [2] git 1-pass → `raw/commits.csv`, `refs.csv`, `edges.csv` (`internal/extract`)
- [x] content-address 폴더 네이밍 + 캐시 스킵 (`internal/paths`, `cmd/gbg`)
- [x] CLI `gbg ingest <url|path> [--data-dir] [--default-branch] [--force]`
- [x] 단위 테스트(parsePR, ParseRepoRef, RunDir) + Makefile
- **검증 결과 (go-wemix 로컬 클론):**
  - 클론 `rev-list --all` = 추출 커밋 **14,520** 정확히 일치(누락 0)
  - branches=4(dev=default), tags=169, edges=17,400, refs=173
  - CSV RFC4180 정합(Python csv 로드 시 컬럼 수 일정), 캐시 재실행 스킵 동작
  - 실행 ~1.1s (bare+blobless, 14.5k 커밋)
  - 주의: 로컬 경로 클론은 소스 `refs/heads/*`만 미러 → 원격 URL이면 전 브랜치 미러됨

> **참고:** 최초 계획의 "14,532"는 소스의 remote-tracking 포함 `--all` 수치.
> 클론이 담은 실제 그래프 기준(14,520)으로 정합 검증됨.

### M2 — Ontology + SQLite/JSON — ✅ 완료 (2026-07-21)
- [x] 레인 배정: 위상 정렬(Kahn, committed_at 우선) + greedy 열 재사용 (`ontology/order.go`, `lanes.go`)
- [x] 색 배정: default 고정 primary(#39d353) + 해시 팔레트 + 미소속 중립(#8b949e) (`color.go`)
- [x] 머지 판별(부모 수 기반, edge_type=commit/merge) — **스쿼시/체리픽은 M4로 이월**(아래 참고)
- [x] containment: **비트셋 위상 전파**(git --contains 호출 0회, in-memory 1-pass) (`containment.go`)
- [x] `graph.json`(렌더용) + `graph.sqlite`(질의용) 생성 (`ontology/jsonout.go`, `db/sqlite.go`)
- [x] 표준 재실행 `gbg ontology <run-dir>` (CSV 경계 분리, `internal/loader`)
- [x] 단위 테스트(topoOrder/lanes/colors/containment) — 소형 합성 그래프
- **검증 결과 (go-wemix):**
  - nodes=14,520 edges=17,400, JSON↔SQLite 커밋 수 일치
  - 레인 22개, 트렁크(lane0) release→dev 색 전환, 머지 유입 엣지 레인 교차(`1→0`) 확인
  - 색: dev=#39d353, release/fix 각기 구분색, 미소속=#8b949e
  - SQL 역질의 검증: "이 커밋 포함 태그/브랜치", "dev∖release 차집합", "PR#172 포함 태그=w0.10.13" 모두 정확
  - 실행 ~4–5s (ontology 단계), graph.json **11MB**

> **⚠️ 이월/이슈 (M4에서 처리):**
> 1. **스쿼시/체리픽 edge_type**: patch-id는 blob이 필요한데 blobless 클론이라 원격에서
>    대량 blob 재요청을 유발 → M2 스킵. M4 enrich에서 **PR head_ref + merge_method(API)**로 판별.
> 2. **graph.sqlite = 239MB** (containment 122만 행 × 40자 SHA 반복). 로컬 분석엔 무방하나
>    **M4 브라우저 sql.js에는 과대**. 해결책: containment를 **정수 id 정규화**
>    (commit_id/ref_id INTEGER)로 재설계하면 수십 MB로 축소. M4 착수 시 적용.
>    (전체 containment는 정확 — 축소는 표현 최적화이지 데이터 손실 아님.)

### M3 — GUI (렌더 MVP) — ✅ 완료 (2026-07-21)
- [x] `gbg serve`(Go, `internal/serve`): `/api/runs`, `/api/runs/:id/graph.json`,
      `/api/runs/:id/containment?sha=`(**서버사이드 SQLite 질의**), 정적 SPA 호스팅, path-traversal 차단
- [x] Svelte SPA(`web/`, Svelte 5 runes + Vite + Tailwind): run 선택 → graph.json 로드 → SVG 스윔레인
- [x] 좌표 Y=노드 배열 인덱스×rowH, X=lane×laneW (색/링크는 graph.json 선계산값 그대로)
- [x] 머지=이중 링 + 레인 교차 엣지, first-parent 굵게, 태그 ◇ / 브랜치 ● 라벨
- [x] 호버 툴팁(SHA·제목·author·PR·branchOf·포함 브랜치) + **호버 시 tags를 `/api/containment` 지연 질의**
- [x] GitHub 하이퍼링크(node.links.commit), 세로 **뷰포트 가상화**(ResizeObserver + 스크롤 윈도잉)
- [x] Tailwind(크롬) + 인라인 SVG(마크), CSS 변수 라이트/다크
- **검증 결과:**
  - `gbg serve` 3개 엔드포인트 정상(runs 2건, graph.json 14,520노드, containment 서버질의 PR#172→w0.10.13), traversal 400 차단
  - Svelte 빌드 무경고, **JS 49KB(gzip 19KB)**, `svelte-check` 0 에러/0 경고
  - Go가 SPA+API 한 프로세스 서빙 확인(index.html·자산·API 모두 200)
  - **시각 미리보기 아티팩트**: 실제 go-wemix 최근 420커밋 렌더(레인/색/머지/태그/호버/링크) — 사용자 확인용

> **개발/실행:** 개발은 `cd web && npm run dev`(Vite 5173 → `/api` 프록시 → `gbg serve :8080`).
> 배포는 `npm run build` → `gbg serve --web-dir web/dist`. (embed.FS 단일바이너리는 패키징 단계에서.)

### M4 — Enrich + 질의 UI — ✅ 완료 (2026-07-22)
- [x] **오프라인 병합 분류**(토큰 불필요): `pr_num` + 부모 수 → squash/merge. edge_type=squash(GUI 점선) + node.mergeMethod. **M2 이월 스쿼시 판별 해소**
- [x] **온라인 enrich**(`internal/enrich`, GraphQL 배치 40/req): state·base/head_ref·url·CI rollup, mergeCommit 부모수로 권위 판별. 토큰은 env→`gh auth token`
- [x] graceful degrade: 토큰 없으면 enrich 스킵, 오프라인 분류만으로 그래프 완성(`--no-enrich`로도 확인)
- [x] `--repo owner/name` 오버라이드(로컬 경로 org 오추론 교정) → 링크·enrich 타깃 정정
- [x] `raw/prs.csv` + sqlite `prs`/`checks` 채움
- [x] serve 역질의 엔드포인트: `/prs?method=&state=`, `/diff?in=&notin=`(릴리즈 차집합)
- [x] Svelte 질의 패널(Release diff / PRs by method 탭) + 툴팁 병합방식·CI 배지
- **검증 결과 (go-wemix, 실제 `gh` 토큰):**
  - 오프라인: 3324 PR 분류(squash 3292 / merge 32), 즉시
  - enrich: 실제 wemixarchive PR 70개 확인(#186 squash·head=fix/sync-check-work-regression·CI SUCCESS, #172 merge·base=master), 나머지 `(#N)`은 go-ethereum 업스트림 번호라 null→degrade
  - graph.json: squash 엣지 3292(점선 렌더), CI 노드 24, node.mergeMethod 채움
  - serve 질의 정상(squash+merged 필터→실제 PR), svelte-check 0/0, JS 56KB(gzip 21KB)

> **포크 주의:** go-wemix는 go-ethereum 포크라 대다수 `(#N)`이 업스트림 PR 번호.
> 오프라인 분류는 구조 기반이라 유효하나, 비검증 PR의 링크는 best-guess. enrich가 실제 PR만 확정.
> 비포크 저장소에선 대부분 enrich로 검증됨.

### M5 — 마감 — ✅ 완료 (2026-07-22)
- [x] **embed.FS 단일 바이너리**: `internal/webui`가 `web/dist`를 내장 → `gbg serve`가
      `--web-dir` 없이 SPA+API 단독 서빙. `make binary`(vite build→copy→go build). 바이너리 15MB
- [x] serve 우선순위: 내장 FS > `--web-dir` > API-only, SPA 폴백 라우팅
- [x] **원격 URL 회귀**: `https://github.com/octocat/Hello-World` blobless 클론 →
      URL org/repo 파싱 + **원격 default 브랜치 자동 감지(master)** + 전체 파이프라인, 0.9s
- [x] **성능/전송**: gzip 미들웨어 → graph.json **11.7MB → 1.43MB(8.2×)**, 무결성 확인.
      렌더는 세로 뷰포트 가상화(M3)로 14.5k 노드도 보이는 창만 그림
- [x] Makefile: `build`/`web`/`binary`/`run`/`test`/`vet`/`clean`
- **검증:** 내장 바이너리가 index.html·자산·SPA폴백·API 모두 서빙, gzip 8.2× 무결,
  원격 클론 default 자동, go test/vet·svelte-check 0/0

## 완료 요약 (M1–M5)
전 파이프라인 동작: URL → bare/blobless clone → git 1-pass(raw CSV) →
enrich(PR/CI, 선택) → ontology(레인·색·머지분류·containment) → graph.json + graph.sqlite →
`gbg serve`(단일 바이너리, gzip) → Svelte SVG 스윔레인(호버·GitHub 링크·역질의 패널).

**선택적 후속:** 체리픽 판별(patch-id/blob), graph.sqlite 정수 id 정규화(서버질의라 비긴급),
브랜치 하이라이트/필터 UI 확장, CI 배지 릴리즈 대시보드화.

## MVP 정의 (최소 가치)
**M1 + M2 + M3** = "URL 넣으면 그 시점 스냅샷을 스윔레인으로 보고, 호버·링크가 된다."
Enrich(M4)는 없어도 git 정보만으로 그래프가 성립하도록 설계(§01 degrade 원칙).

## 착수 순서
1. `package.json` + TS 세팅, `src/` 골격
2. M1 Extract CLI (`ingest <url>`) — 가장 먼저 눈에 보이는 raw CSV
3. M2 Ontology — graph.json/sqlite
4. M3 웹 렌더

## 리스크 & 대응
| 리스크 | 대응 |
|---|---|
| 스쿼시 이력 소실로 유입 브랜치 특정 난망 | PR `head_ref`(API) + patch-id 조합, 불확실 시 `squash?` 표기 |
| 대규모 저장소 렌더 성능 | 기본 뷰포트 제한 + 가상화 + 시간 윈도우 |
| GitHub rate-limit/토큰 부재 | git-only degrade, 토큰 로테이션(gitfut tokens.ts) |
| `git tag --contains` 비용(태그·커밋 많을 때) | 배치화·캐시, 필요 SHA만 지연 계산 |
| 브랜치 decoration 없는 중간 커밋의 브랜치 귀속 | first-parent 체인 상속 규칙(§03) |

## 결정 로그
- **GUI 스택: Svelte SPA + `gbg serve`(Go) + Tailwind** (사용자 확정, 2026-07-21)
  - Next.js 후보 폐기: API routes가 Go와 역할 중복 → 백엔드는 Go 하나로 통일(`gbg serve`가 서블릿 역할)
  - React 대신 Svelte: 컴파일러 방식·VDOM 없음 → SVG 데이터비주얼에 유리
  - SvelteKit 대신 plain Svelte SPA: 서버 기능 불필요(Go가 담당)
  - 239MB SQLite는 브라우저 미로드 → Go 서버사이드 질의(`/api/query`)로 해결, 정수 id 정규화 긴급도 하락
- **코어 로직 Go** — go-wemix 동일 언어, 단일 바이너리·성능. `data/` 파일 경계로 분리 (사용자 지시, 2026-07-21)
- SQLite는 순수 Go 드라이버 `modernc.org/sqlite` 사용 → CGO 없이 정적 바이너리 유지
- SQL을 처음부터 1급 산출물로 포함 (사용자 지시, 2026-07-21)
- 레인=위상 기반 동적, 색=브랜치 결정적 매핑 (사용자 확정)
- 고정 브랜치 없음, default 브랜치가 시작 앵커 (사용자 확정)
- clone은 bare + blobless (go-wemix 174MB 실측 근거)
