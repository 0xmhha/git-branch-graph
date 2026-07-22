# Git Branch Graph — 프로젝트 계획

## 위치
- 문서: `study/docs/projects/git-branch-graph/`
- 코드: `new-project/git-branch-graph/` (git init 완료, `main` 브랜치)

## 기술 스택
| 영역 | 선택 | 근거 |
|---|---|---|
| 언어/런타임 | Node.js (TypeScript) | gitfut 스택과 통일, git CLI 호출 용이 |
| 웹 | Next.js | gitfut 청사진 재사용 (route=수집, page=렌더) |
| GitHub 수집 | GraphQL (`lib/github/client.ts` 패턴) | 배치 호출, rate-limit 관리 |
| git 접근 | `git` CLI 서브프로세스 | bare/partial clone, 1-pass log |
| 저장 | CSV(raw) + JSON(렌더) + SQLite(질의) | 계층 분리, SQL 1급 |
| SQLite | `better-sqlite3`(생성) + `sql.js`(브라우저 질의) | 서버리스 파일 DB |
| 렌더 | SVG + React | 호버·하이퍼링크·접근성 |

## 마일스톤

### M1 — Extract 파이프라인 (로컬만, 네트워크 0)
- [1] bare + blobless clone / fetch 증분
- [2] git 1-pass → `raw/commits.csv`, `refs.csv`, `edges.csv`
- content-address 폴더 네이밍 + 캐시 스킵
- **검증:** go-wemix URL로 raw CSV 생성, 커밋수 14,532 일치 확인

### M2 — Ontology + SQLite/JSON
- 레인 배정 알고리즘 구현
- 색 배정(default 고정 + 해시 팔레트)
- 머지/스쿼시/체리픽 판별(부모수 + patch-id)
- containment 계산 (`git tag/branch --contains`)
- `graph.json` + `graph.sqlite` 생성 (스키마: `02-data-model.md`)
- **검증:** 대표 역질의 SQL 4종 동작, JSON↔SQLite 카운트 일치

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
- SQL을 처음부터 1급 산출물로 포함 (사용자 지시, 2026-07-21)
- 레인=위상 기반 동적, 색=브랜치 결정적 매핑 (사용자 확정)
- 고정 브랜치 없음, default 브랜치가 시작 앵커 (사용자 확정)
- clone은 bare + blobless (go-wemix 174MB 실측 근거)
