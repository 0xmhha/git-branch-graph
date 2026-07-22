# Git Branch Graph — 선택적 후속 (Backlog)

M1–M5로 전 파이프라인이 동작한다. 아래는 **필수는 아니지만 가치 있는 후속 항목**이다.
각 항목은 *무엇 / 왜 미뤘나 / 접근 / 영향 범위 / 우선순위*로 정리한다.

우선순위 표기: **P1**(사용자 체감 큼) · **P2**(상황따라 유용) · **P3**(엣지/니스투해브)

---

## A. 분류 정확도 (Correctness)

### A1. 체리픽 판별 (cherry-pick) — P2
- **무엇:** 같은 변경이 여러 브랜치에 개별 커밋으로 복제된 경우(백포트)를 `cherry`
  edge_type으로 표기. 현재 edge_type은 `commit`/`merge`/`squash`까지만.
- **왜 미뤘나:** 신뢰성 있는 판별에 `git patch-id`(diff 내용 해시)가 필요한데,
  이는 **blob이 있어야** 계산된다. 우리는 blobless 클론이라 원격에서 blob 대량 재요청을
  유발 → blobless 효율을 무너뜨림 (M2/M4에서 명시적으로 보류).
- **접근:**
  1) *옵트인 blob 페치*: `--with-blobs` 플래그로 `git fetch` 후 patch-id 계산.
  2) *경량 근사*: (author, subject, 변경파일 목록) 지문으로 후보만 뽑고 표기는 `cherry?`.
  3) *GitHub API*: PR의 `associatedPullRequests`/커밋 relation으로 일부 backport 추적.
- **영향:** `internal/ontology`(edge_type), `internal/enrich`(옵션), GUI는 이미
  `cherry` 점선-점 렌더 준비됨(M3).

### A2. 리베이스 구분 (rebase vs squash) — P3
- **무엇:** 현재 단일 부모 랜딩은 모두 `squash`로 표기. GitHub rebase 병합도 단일 부모라
  `squash`로 오분류될 수 있음.
- **왜 미뤘나:** GitHub API가 사후에 병합 방식을 직접 노출하지 않음. mergeCommit 부모 수로는
  squash와 rebase가 동일(1).
- **접근:** rebase는 PR의 head 커밋들이 base 히스토리에 **개별 보존**됨(subject 다수 일치).
  enrich에서 PR `commits`를 받아 base 포함 여부로 rebase 추정. 비용 대비 가치 낮음.
- **영향:** `internal/enrich`, `classify.go`.

### A3. 포크 PR-링크 검증 (fork caveat) — P2
- **무엇:** go-wemix 같은 **포크**에선 대다수 `(#N)`이 업스트림(go-ethereum) PR 번호.
  오프라인 분류는 구조 기반이라 유효하나, 비검증 PR의 링크(`/pull/N`)는 best-guess라
  엉뚱한 PR로 갈 수 있음.
- **왜 미뤘나:** 오프라인에서 업스트림/자체 PR을 구분 불가. enrich가 실제 PR만 확정하지만,
  비검증분의 링크를 그대로 둠.
- **접근:** enrich로 확인된 PR만 `verified=true` 표시, 미확인 PR은 링크를 숨기거나
  `unverified` 배지. graph.json 노드에 `prVerified` 필드 추가.
- **영향:** `classify.go`, `jsonout.go`, GUI 툴팁.

---

## B. 저장/성능 (Storage & Performance)

### B1. graph.sqlite 정수 id 정규화 — P2
- **무엇:** containment 122만 행 × 40자 SHA 반복으로 `graph.sqlite`가 **239MB**.
  commit_id/ref_id INTEGER로 정규화하면 수십 MB로 축소.
- **왜 미뤘나:** **아키텍처가 이미 문제를 우회함** — 브라우저는 DB를 로드하지 않고
  Go가 서버사이드로 질의(`/api/*`). 따라서 로컬 아티팩트 크기 최적화일 뿐 긴급도 낮음.
- **접근:** `commits`에 `id INTEGER`, `refs`에 `id`, `containment(commit_id, ref_id)`.
  질의는 조인으로. 02-data-model 스키마 개정 동반.
- **영향:** `internal/db`, 02-data-model.md. sql.js 정적 배포(B3)를 하려면 선행 필요.

### B2. containment 부분 계산 옵션 — P3
- **무엇:** 초대형 저장소에서 containment 전체(122만 행)가 부담이면, PR-보유 커밋 또는
  최근 N개월로 범위 제한.
- **왜 미뤈나:** 현재 규모(go-wemix)는 계산·저장 모두 감당 가능(비트셋 1-pass).
- **접근:** `--containment=full|pr-only|recent:N` 플래그. **범위 제한 시 반드시 로그로
  드롭 내역 명시**("no silent caps" 원칙).
- **영향:** `internal/ontology/containment.go`, CLI.

### B3. sql.js 순수 정적 배포 폴백 — P3
- **무엇:** `gbg serve` 없이 graph.json+sqlite를 정적 호스팅하고 브라우저 sql.js(WASM)로
  질의. 서버 없는 공유용.
- **왜 미뤘나:** 기본 배포는 단일 바이너리(`gbg serve`)로 충분. sql.js는 239MB DB를
  브라우저가 로드해야 해 **B1(정규화) 선행 필수**.
- **영향:** `web/`(sql.js 통합), 배포 문서.

---

## C. GUI 기능 (UX)

### C1. 브랜치 하이라이트 / 필터 — P1
- **무엇:** 특정 브랜치 클릭 시 해당 라인 강조·나머지 흐리게, 브랜치/태그/PR 필터.
- **왜 미뤘나:** M3는 렌더 MVP에 집중. 데이터는 이미 노드에 `branchOf`/`refs` 있음.
- **접근:** Svelte 상태로 `highlightBranch`, 노드/엣지 opacity 조절. 헤더에 필터 컨트롤.
- **영향:** `web/src/lib/Swimlane.svelte`, `App.svelte`. 프론트만.

### C2. 뷰포트 컨트롤 (기간/커밋수) — P1
- **무엇:** "최근 N커밋 / 최근 M개월" 슬라이더로 렌더 범위 조절, 특정 SHA로 점프.
- **왜 미뤘나:** 현재는 전체 로드 후 가상화. 대형 저장소에서 초기 범위 제한이 UX에 유리.
- **접근:** 클라이언트 측 범위 슬라이싱(데이터는 이미 전부 로드). 또는 serve에
  `?since=&limit=` 범위 파라미터.
- **영향:** `web/`, (옵션) `internal/serve`.

### C3. 릴리즈 대시보드 (환경별 상태) — P2
- **무엇:** `w0.10.x` / `_devnet` / `_testnetboot` 태그를 나란히 놓고 "어느 환경에 무엇이
  배포됐나" + CI 상태 표. 최초 상담의 핵심 요구(릴리즈/버전 상태 체크)의 완성형.
- **왜 미뤘나:** M4에서 데이터(containment, CI, merge_method)는 다 갖췄고, 전용 뷰만 미구현.
- **접근:** serve에 `/api/runs/:id/releases`(태그별 포함 PR·CI 집계) + Svelte 대시보드 탭.
- **영향:** `internal/serve`, `web/`. **최초 사용자 요구와 직결 → 가치 높음.**

### C4. "이 수정 어디 들어갔나" 역방향 조회 UI — P2
- **무엇:** SHA/PR 번호 입력 → 포함 브랜치·태그·환경 한 번에. 백엔드(`/containment`)는
  이미 있음, 전용 검색 UI만 추가.
- **접근:** 질의 패널에 검색 탭 추가.
- **영향:** `web/src/lib/QueryPanel.svelte`.

---

## D. 운영 (Ops)

### D1. run 폴더 관리 / 정리 — P3
- **무엇:** `data/`에 실행별 폴더가 누적(현재 stale run 존재). 목록·삭제·최신만 유지.
- **접근:** `gbg runs list|rm`, serve에 삭제 엔드포인트(주의: 파괴적).
- **영향:** CLI, `internal/serve`.

### D2. enrich 캐시 / 증분 — P3
- **무엇:** enrich가 매번 전 PR 조회(go-wemix 3324건 → 41s). PR 상태는 잘 안 변하므로 캐시.
- **접근:** PR 응답을 `data/<run>/enrich-cache.json`에 저장, 변경분만 재조회.
- **영향:** `internal/enrich`.

### D3. 원격 default 브랜치 확정 개선 — P3
- **무엇:** 로컬 경로 ingest 시 default가 소스 HEAD를 따름(예: release/w0.10.14).
  `--default-branch`/`--repo`로 교정 가능하나, 소스의 `origin/HEAD`를 읽어 자동 보정 가능.
- **접근:** acquire에서 소스 저장소의 `symbolic-ref refs/remotes/origin/HEAD` 참조.
- **영향:** `internal/acquire`.

---

## 추천 착수 순서
가치·비용 기준:
1. **C3 릴리즈 대시보드** — 최초 사용자 요구 직결, 데이터 이미 확보 (P2, 프론트+얇은 serve)
2. **C1 브랜치 하이라이트/필터** — 체감 큼, 프론트만 (P1)
3. **C2 뷰포트 컨트롤** — 대형 저장소 UX (P1, 프론트 위주)
4. **A3 포크 PR-링크 검증** — 오분류 링크 방지 (P2, 소규모)
5. 이후 A1 체리픽 / B1 정규화 등 필요 시.
