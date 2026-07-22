# git-branch-graph

임의의 GitHub 저장소 URL을 입력하면, 그 시점의 브랜치·커밋·머지·PR·태그 상태를
**동적으로 분석**하여 커밋 위상(topology) 기반 **스윔레인 그래프**로 보여주는 도구.

## 무엇을 하나
```
GitHub URL
  → bare + blobless clone (재실행 시 fetch 증분)
  → git 1-pass 스캔으로 raw CSV 추출
  → GitHub API로 PR/CI 보강 (git이 못 주는 것만)
  → 온톨로지 계산(레인·색·머지판별·containment·링크)
  → graph.json + graph.sqlite 생성
  → data/<org>__<repo>__<branch>__<sha7>/ 저장
  → GUI가 폴더를 읽어 SVG 타임라인 그래프로 렌더 (호버·하이퍼링크)
```

## 특징
- **고정 브랜치 없음** — URL 시점 스냅샷을 동적 분석. default 브랜치가 시작 앵커.
- **위상 기반 레인 + 브랜치 결정적 색** — 라인 구분 명확.
- **머지 vs 스쿼시 vs 체리픽 판별** — 부모 수 + patch-id + PR 메타.
- **SQL 1급** — `graph.sqlite`로 "이 수정 어디 들어갔나" 역질의.
- **호버·GitHub 하이퍼링크** — 커밋/PR/브랜치/태그 페이지로 이동.

## 데이터 계층
raw CSV(사실) → 온톨로지(파생 관계) → JSON(렌더) + SQLite(질의)

## 설계 문서
`study/docs/projects/git-branch-graph/`
- `00-overview.md` — 요구사항·아키텍처 요약
- `01-architecture.md` — 파이프라인 5단계 상세
- `02-data-model.md` — raw CSV + SQLite 스키마 + graph.json
- `03-graph-rendering.md` — 레인 알고리즘·색 규칙·호버·링크
- `04-project-plan.md` — 마일스톤·MVP·리스크

## 상태
설계 완료. 구현 착수 전 (M1 Extract 파이프라인부터).
