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
| **웹/대시보드** | **Next.js (Node/TypeScript)** | gitfut 청사진 재사용, `data/` 읽어 렌더 |
| SQLite (브라우저 질의) | `sql.js` (WASM) | 서버 없이 containment 역질의 |
| 렌더 | SVG + React | 호버·하이퍼링크·접근성 |

### 코어(Go) ↔ 웹(Node) 경계
```
[Go 코어 gbg]  acquire → extract → enrich → ontology → (graph.json + graph.sqlite)
                                                    │  data/<run>/ 에 기록
                                                    ▼
[Node 웹]      data/ 폴더를 읽어 SVG 렌더 + sql.js 질의
```

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

### M3 — GUI (렌더 MVP)
- `graph.json` 로드 → SVG 스윔레인
- 색 규칙 적용, 머지/분기 엣지, 태그/브랜치 라벨
- 호버 툴팁 + GitHub 하이퍼링크
- 기본 뷰포트(최근 N커밋) + 브랜치 하이라이트
- **검증:** go-wemix 그래프에서 dev/master/release/fix 라인·스쿼시 엣지 육안 확인

### M4 — Enrich + 질의 UI
- [3] GitHub GraphQL PR/CI 보강 → `prs.csv`, `checks.csv`
- 병합방식 뱃지, CI 상태 표시
- sql.js 애드혹 질의 패널 (containment 역질의)
- 토큰 부재 시 graceful degrade 확인

### M5 — 마감
- 대규모 성능(뷰포트 가상화), 에러 처리, README
- 여러 저장소 URL 회귀 테스트

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
- **코어 로직 Go, 프론트 Node(Next.js)** — go-wemix 동일 언어, 단일 바이너리·성능. `data/` 파일 경계로 분리 (사용자 지시, 2026-07-21)
- SQLite는 순수 Go 드라이버 `modernc.org/sqlite` 사용 → CGO 없이 정적 바이너리 유지
- SQL을 처음부터 1급 산출물로 포함 (사용자 지시, 2026-07-21)
- 레인=위상 기반 동적, 색=브랜치 결정적 매핑 (사용자 확정)
- 고정 브랜치 없음, default 브랜치가 시작 앵커 (사용자 확정)
- clone은 bare + blobless (go-wemix 174MB 실측 근거)
