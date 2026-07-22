# Git Branch Graph — 렌더링 스펙

레인 배정, 색 규칙, 호버, 하이퍼링크. GUI는 "이미 계산된 것을 그린다"만 하고,
계산은 온톨로지 단계([4])가 소유한다.

---

## 1. 좌표계
- **Y축 = 시간(내림차순)**: 최신 커밋이 위. 정렬 키 = `committed_at` 내림차순 + 위상
  정렬 tie-break(부모가 자식보다 항상 아래).
- **X축 = 레인(lane)**: 커밋 위상에 따라 동적 배정된 정수 열. 레인은 브랜치 고정이 아님.
- 시작 앵커: **default 브랜치 tip**을 `lane 0`, 화면 최상단 기준으로 배치.

---

## 2. 레인 배정 알고리즘 (위상 기반, greedy 열 재사용)
목표: 라인 교차 최소화 + 하나의 개발 라인이 최대한 같은 열 유지.

```
입력: 커밋들(시간 내림차순), edges(부모관계)
active_lanes = []            # 열 인덱스 → 그 열이 "기다리는" 다음 부모 sha
for commit in commits (위에서 아래로):
    # 1) 이 커밋을 기다리던 열 찾기 (없으면 새 열 할당)
    lane = active_lanes.index_of(commit.sha)  or  free_column()
    commit.lane = lane
    parents = commit.parents
    if parents:
        # first-parent 는 같은 열을 계승
        active_lanes[lane] = parents[0]
        # 추가 부모(머지 유입)는 각각 새 열 or 기존 열에 배치 → merge 엣지
        for p in parents[1:]:
            plane = free_column();  active_lanes[plane] = p
    else:
        free(lane)               # root 커밋 → 열 반납
```
- **first-parent 계승** = 하나의 브랜치 라인이 세로로 곧게 유지되는 핵심.
- **머지 커밋**: 유입 부모(parent_index≥1)는 다른 레인에서 들어오는 곡선 엣지로.
- **분기(branch out)**: 부모 방향에서 여러 자식이 갈라지면 자식들이 새 레인 확보.
- 열 반납/재사용으로 화면 폭이 무한정 넓어지지 않게 한다.

> 표준 git 그래프(gitk/Git Graph)와 동일 계열 알고리즘. 사용자가 "커밋 위상 기반 동적
> 배정"에 동의했으므로 이 방식을 채택.

---

## 3. 색 규칙 (라인 구분)
색은 **레인이 아니라 브랜치**에 결정적으로 매핑한다 → 같은 브랜치는 화면 어디서든 같은 색.

| 규칙 | 내용 |
|---|---|
| **default 브랜치** | 고정 primary 색 (예: 초록 `#39d353`). 항상 동일 |
| **일반 브랜치** | `hash(branch_name) % palette.length` → 결정적 색. 재실행에도 안정 |
| **팔레트** | 색맹 안전·라이트/다크 대비 확보한 정성 팔레트(8~12색). `dataviz` 스킬 기준 준용 |
| **태그** | 색이 아니라 **형태**로 구분(마름모/뱃지) — 색 채널 과부하 방지 |
| **엣지 색** | 자식 커밋의 브랜치 색을 따름. 머지 유입 엣지는 유입원 색 |
| **커밋 상태** | 색조가 아니라 **채도/외곽선**으로 보조 표현(예: 미릴리즈=점선 테두리) |

`branch_of`(first-parent 귀속 브랜치)를 색 키로 사용. 브랜치 tip decoration이 없는
중간 커밋도 first-parent 체인을 타고 대표 브랜치 색을 상속.

---

## 4. 노드/엣지 시각 문법
| 요소 | 표현 |
|---|---|
| 일반 커밋 | 채운 원 (브랜치 색) |
| 머지 커밋 | 두 줄 외곽 원 / 큰 원 |
| 스쿼시 유입 | **점선** 엣지 + `squash` 마이크로 뱃지 |
| 체리픽 | 파선(dash-dot) 엣지 + `cherry` 뱃지 |
| 브랜치 tip | 원 + 브랜치명 라벨(색 배경 pill) |
| 태그 | 마름모 + 태그명 (환경변형 `_devnet` 등은 보조 라벨) |
| default 브랜치 라인 | 굵은 선 강조 |

---

## 5. 호버 인터랙션
SVG 노드/엣지는 DOM이므로 호버 툴팁·링크가 자연스럽다.

**커밋 노드 호버:**
```
┌─────────────────────────────────────┐
│ 11eb943d  release/w0.10.14           │
│ chore: bump version to v0.10.14      │
│ alice · 2026-07-10 09:05             │
│ PR #— · merge: —                     │
│ 포함 태그: (없음)                     │
│ 포함 브랜치: release/w0.10.14         │
└─────────────────────────────────────┘
```
- SHA(단축), 대표 브랜치, subject, author·시각, PR 번호·병합방식,
  `contained_in_tags/branches`(온톨로지 계산).

**엣지 호버:** `fromLane 브랜치 → toLane 브랜치`, edge_type(commit/merge/squash/cherry).

---

## 6. 하이퍼링크
각 요소를 `<a href>`로 감싼다. `linkBase = https://github.com/<org>/<repo>`.

| 클릭 대상 | URL |
|---|---|
| 커밋 노드 | `{linkBase}/commit/{sha}` |
| PR 뱃지/번호 | `{linkBase}/pull/{pr_num}` |
| 브랜치 라벨 | `{linkBase}/tree/{branch}` |
| 태그 마름모 | `{linkBase}/releases/tag/{tag}` (없으면 `/tree/{tag}`) |
| 비교(옵션) | `{linkBase}/compare/{base}...{head}` |

링크는 온톨로지 단계에서 노드별 `links{}`로 미리 조립 → GUI는 부착만.

---

## 7. 렌더 기술 선택
- **SVG + React** (Canvas 아님): 노드가 DOM → 호버·`<a>`·접근성 자연 처리.
- 대규모(수천 노드) 성능: 뷰포트 가상화(보이는 Y범위만 렌더) + 커밋 시간 윈도우
  필터(예: 최근 N개월 / HEAD로부터 M커밋)로 기본 범위 제한, 확장 로드.
- 애드혹 질의 패널(옵션): `graph.sqlite`를 **sql.js(WASM)** 로 브라우저에서 직접 질의
  → 서버 없이 containment 역질의 UI 제공.

---

## 8. 기본 뷰포트 정책
- 초기: default 브랜치 tip 기준 최근 구간(예: 200 커밋 또는 90일)만.
- 브랜치가 많을 때: 활성(최근 커밋 있는) 브랜치 우선, 나머지는 접기.
- 사용자 컨트롤: 기간/커밋수 슬라이더, 특정 브랜치 하이라이트, PR/태그 필터.
