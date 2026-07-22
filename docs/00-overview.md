# Git Branch Graph — 개요

> 임의의 GitHub 저장소 URL을 입력하면, 그 시점의 브랜치·커밋·머지·PR·태그 상태를
> **동적으로 분석**하여, 커밋 위상(topology) 기반 **스윔레인 그래프**로 보여주는 도구.

## 한 줄 정의
GitHub URL → (bare/blobless clone + GitHub API) → raw CSV → 온톨로지 계층 →
graph-friendly 산출물(`graph.json` + `graph.sqlite`) → GUI가 `data/` 폴더를 읽어
SVG 타임라인 그래프로 렌더.

## 핵심 요구사항 (사용자 확정)
1. **고정 브랜치 없음** — 브랜치는 계속 변하므로 특정 브랜치를 하드코딩하지 않는다.
   URL 입력 시점에 동적으로 분석하고, 그 시점 상태로 표기한다.
2. **시작점 = default 브랜치** — GitHub repo의 default branch를 항상 그래프의 시작
   브랜치로 둔다.
3. **레이아웃** — 핵심 브랜치들이 타임라인 기반(세로)으로 나오고, 커밋·머지·분기에
   의해 "어느 브랜치로 들어가고 나왔는지"가 명확히 보인다.
4. **커밋 위상 기반 레인 배정** — 레인은 브랜치 고정이 아니라 커밋 위상에 따라 동적
   배정한다. 브랜치별 **색상**은 레인/브랜치를 **구분**하기 위한 규칙이다.
5. **색 규칙** — 라인 구분이 명확하도록 색 사용 규칙을 정하고, 그 규칙에 의해 확인
   가능해야 한다.
6. **호버 인터랙션** — 노드/엣지에 마우스 호버 시 핵심 정보가 보인다.
7. **하이퍼링크** — 노드에서 GitHub repo의 해당 커밋/PR/브랜치/태그 페이지로 이동하는
   링크가 걸려 있다.

## 데이터 계층 (사용자 확정 아키텍처)
```
URL 입력
  │
  ▼  ① bare + blobless clone (재실행 시 fetch 증분)
git 로컬 그래프 ──② 1-pass 스캔──► raw CSV (nodes/edges 스키마)
       │                                  ▲
       │                        ③ GitHub GraphQL 배치 (PR/CI/링크만)
       ▼
  ④ 온톨로지 계층 (레인·색·머지판별·containment·링크 계산)
       ▼
  ⑤ graph-friendly 산출물: graph.json  +  graph.sqlite
       ▼
  ⑥ data/<org>__<repo>__<defaultBranch>__<headSha7>/ 에 저장
       ▼
  ⑦ GUI: data/ 폴더(또는 URL) 선택 → 산출물 로드 → SVG 스윔레인 렌더
```

## 효율화 결정 (근거: go-wemix 실측)
go-wemix 기준 `.git` 174MB / 오브젝트 106,543개 / 커밋 14,532개 / default=`dev`.

| 결정 | 이유 |
|---|---|
| **bare + `--filter=blob:none` clone** | 파일 blob이 174MB 대부분. 그래프엔 blob 불필요 → 수 MB로 축소, 그래프는 그대로 |
| **git 1-pass 수집** | 커밋/부모/refs/태그는 `git log --all` 한 번으로. API 페이지네이션·rate-limit 회피 |
| **GitHub API는 보강만** | PR 병합방식·상태·리뷰·CI·정규 URL 등 git이 못 주는 것만 GraphQL 배치로 |
| **content-address 폴더명** | default HEAD SHA로 명명 → HEAD 불변 시 재수집 스킵(캐시) |
| **JSON + SQLite** | JSON은 GUI 직접 로드용, SQLite는 역질의(containment 등)용. 그래프DB 서버는 과함 |

## 문서 구성
- `00-overview.md` — 이 문서 (요구사항·아키텍처 요약)
- `01-architecture.md` — 파이프라인 상세, 모듈 경계, 실행 흐름
- `02-data-model.md` — raw CSV 스키마 + 온톨로지 계층 + SQLite 스키마(핵심)
- `03-graph-rendering.md` — 레인 배정 알고리즘, 색 규칙, 호버/링크 스펙
- `04-project-plan.md` — 마일스톤, 디렉토리 구조, MVP 범위

## 레퍼런스
- **gitfut** (`references/gitfut`) — Next.js + GitHub GraphQL 소비 패턴의 청사진.
  `lib/github/client.ts`(GraphQL 클라이언트), `lib/github/tokens.ts`(토큰 관리),
  `app/api/*/route.ts`(수집 엔드포인트).
- **go-wemix** (`wemix/go-wemix`) — 복잡한 브랜치 전략의 실사용 대상.
  `dev`(default)→`master`(태그 릴리즈), `release/w*`, `devnet`, `fix/*`, 스쿼시 중심 머지,
  `w0.10.x` + 환경 변형(`_devnet`/`_testnetboot`) 태그.
