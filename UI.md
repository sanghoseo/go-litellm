# UI 문서

`ui/` 폴더는 LiteLLM Proxy의 관리 대시보드 (Admin UI)를 담고 있다. Next.js App Router 애플리케이션을 **static export** 로 빌드해서 nginx가 서빙하는 구조다.

## 1. 폴더 구조

```
ui/
├── Dockerfile              # Node 빌드 → nginx static 서빙 이미지
├── nginx.conf              # /ui 프리픽스 라우팅, RSC payload 서빙/캐시 규칙
└── litellm-dashboard/      # Next.js 애플리케이션 루트
    ├── package.json
    ├── next.config.mjs     # output: "export", assetPrefix: /litellm-asset-prefix
    ├── tsconfig.json
    ├── vitest.config.ts    # unit/component/integration/types 4개 테스트 프로젝트
    ├── eslint.config.mjs   # flat config, lint budget/suppression 관리
    ├── knip.json           # 무사용 코드 검사
    ├── build_ui.sh         # 로컬 빌드 후 litellm/proxy/_experimental/out 로 복사
    ├── scripts/
    │   ├── gen-api-types.mjs        # proxy OpenAPI 스펙 → schema.d.ts 생성
    │   ├── check-lint-budgets.mjs
    │   └── eslint-rules/            # 커스텀 ESLint 플러그인
    ├── public/
    ├── tests/              # 테스트 셋업, 단위/컴포넌트 공용 유틸
    └── src/
        ├── app/            # 라우트 (App Router)
        ├── components/     # 공용 컴포넌트 + shadcn 스타일 원시 (components/ui)
        ├── contexts/       # Auth / Theme / ReactQuery / PluginMode / ChatShell
        ├── hooks/
        ├── lib/
        │   ├── http/       # API 클라이언트 (api, client, runtime, schema.d.ts)
        │   ├── forms/
        │   └── toast.ts
        ├── data/           # compliance 프롬프트 데이터
        ├── utils/
        └── types.ts
```

## 2. 기술 스택

| 영역 | 기술 |
|---|---|
| 프레임워크 | Next.js 16 (App Router, `output: "export"`) + React 19 + TypeScript 5.9 |
| 스타일 | Tailwind CSS 4, shadcn 스타일 원시 (`@base-ui/react` 기반, `src/components/ui/`) |
| 데이터 | TanStack Query 5 + `openapi-fetch` + `openapi-react-query` (타입 안전 HTTP 클라이언트) |
| 테이블/폼 | TanStack Table, React Hook Form + zod |
| 기타 | recharts, react-markdown, sonner(toast), nuqs(URL 파라미터), date-fns/moment/dayjs |
| 테스트 | Vitest (jsdom), Testing Library, Playwright (e2e, 리포 루트 `tests/e2e/`) |
| 린트/포맷 | ESLint 9 (flat config + 커스텀 룰), Prettier, Knip |
| 런타임 요구사항 | Node.js >= 24.14.1, npm >= 11.10.0 |

## 3. 라우팅

### 루트 라우트 (`src/app/`)

| 라우트 | 역할 |
|---|---|
| `(dashboard)/` | 메인 대시보드 (레이아웃: Navbar + 사이드바 + ThemeProvider + 인증 게이트). 페이지별 폴더 단위로 구성 |
| `login/` | 로그인 페이지 (인증되지 않으면 `/ui/login`으로 리다이렉트) |
| `onboarding/` | 최초 설정 온보딩 |
| `chat/` | Chat UI (API key, credentials, integrations, logs, usage 서브 페이지) |
| `connect/` | Connect 페이지 |
| `mcp/oauth/` | MCP OAuth 플로우 |
| `model_hub`, `model_hub_table` | 공개 모델 허브 |

### 대시보드 사이드바 메뉴 (`src/components/leftnav.tsx`)

- **AI GATEWAY**: Virtual Keys, Playground, Models + Endpoints, Agentic (Agents / Workflow Runs / Memory), MCP Servers, Skills, Guardrails, Policies, Tools (Search Tools / Vector Stores / Tool Policies)
- **OBSERVABILITY**: Usage, Cost Optimization (Beta), Logs, Guardrails Monitor
- **ACCESS CONTROL**: Teams, Projects (Beta), Internal Users, Organizations, Access Groups, Budgets
- **DEVELOPER TOOLS**: API Reference, AI Hub, Learning Resources, Response Cache, Experimental (Prompts / API Playground / Tag Management / Old Usage)
- **SETTINGS** (admin 전용): Router Settings, Logging & Alerts, Admin Settings, Cost Tracking, UI Theme

메뉴 항목은 `roles` 필드로 권한 기반 표시를 한다. 일부 메뉴는 JS 안에 `/ui/...` URL이 하드코딩되어 있어 nginx에서 `/ui/<page> → out/<page>.html`로 매핑한다.

### 페이지 폴더 컨벤션 (`(dashboard)/README.md`)

페이지마다 폴더 하나, 그 안의 고정 구성:

```
teams/
├── TeamsView.tsx          # 페이지 메인 뷰
├── page.tsx               # 라우트 엔트리
├── components/            # 이 페이지 전용 컴포넌트
├── hooks/                 # 순수 .ts 훅 (useFetchTeams 등)
└── utils.ts
```

- 컴포넌트는 "dumb"하게 유지, 300라인 초과 시 분해
- 공용 컴포넌트/훅은 lowest common ancestor 폴더로 올리기

## 4. HTTP / 인증 레이어

`src/lib/http/`:

- `schema.d.ts` — proxy의 OpenAPI 스펙에서 **자동 생성** (`npm run gen:api`). 절대 수동 수정 금지. 백엔드 route/응답 모델 변경 후 반드시 재생성해서 커밋 (CI "Check UI API Types Sync"가 강제).
- `api.ts` — `openapi-fetch` 기반 타입 안전 클라이언트 (`fetchClient`). 미들웨어가 auth 헤더를 주입하고 non-2xx 응답을 `ApiError`로 변환.
- `runtime.ts` / `resolveApiBase.ts` — base URL은 import 시점에 고정하지 않고 호출 시점에 주입 (split-origin proxy/worker URL 지원).
- `client.ts` — 에러 타입 (`ApiError`) 및 에러 메시지 파싱.

**인증 보안 규칙**: 토큰/API key를 `localStorage`에 절대 저장하지 않는다. `httpOnly` 쿠키 우선, 그다지도 못하면 `sessionStorage` (모든 web storage는 XSS로 읽힐 수 있음). `AuthContext`가 쿠키에서 JWT를 읽어 만료를 검증한다.

## 5. 빌드 & 배포

- `next.config.mjs`: `output: "export"` (정적 사이트), `assetPrefix: "/litellm-asset-prefix"`, `trailingSlash: true`. 프로덕션 빌드 시 `console.log` 제거 (`error`/`warn` 제외).
- `ui/Dockerfile`: 2단계 빌드 — node:24-alpine에서 `npm ci && npm run build` → nginx:1.27-alpine에 `out/`를 web root로 배치. 3000 포트 서빙, `/healthz` 헬스체크.
- `ui/nginx.conf` 핵심:
  - `/litellm-asset-prefix/_next/` → 실제 `_next/` 트리 alias (빌드 시 디렉터리 복제 대신 요청 시점에 매핑)
  - `*.txt` (RSC/flight payload)는 반드시 static export에서 서빙. HTML로 폴백하면 client-side navigation이 끝나지 않아 `/` ⇄ `/ui/login` 무한 리다이렉트 루프가 된다. 존재하지 않는 payload는 404 (hard navigation으로 degrade)
  - `/ui`, `/ui/`, `/ui/<page>` → `page.html` / `page/index.html` → SPA 폴백 `index.html`
  - API 경로(`/v1`, `/key`, `/.well-known/litellm-ui-config` ...)는 앞단 reverse proxy가 gateway/backend로 라우팅. 여기까지 흘러오면 404 (JSON을 기대하는 caller를 혼동시키지 않으려면)
- `build_ui.sh`: 로컬 빌드 후 `litellm/proxy/_experimental/out`에 복사 (모놀리스 배포 형태).

## 6. 테스트

Vitest 4개 프로젝트 (`vitest.config.ts`):

| 프로젝트 | 대상 | 환경 |
|---|---|---|
| `unit` | `*.test.ts` (콜래보레이터를 double로 교체, ms 단위) | node |
| `component` | `*.test.tsx` (단일 컴포넌트 트리) | jsdom |
| `integration` | `*.integration.test.tsx` (실제 컴포넌트 트리, 네트워크 경계만 스텁) | jsdom |
| `types` | `*.test-d.ts(x)` 타입체크 | — |

브라우저 레벨 e2e는 리포 루트 `tests/e2e/` (Playwright, 실행 중인 proxy를 대상으로 수행).

핵심 룰 (상세 규약은 `litellm-dashboard/CLAUDE.md` 참고):

- 테스트해야 할 로직이 있으면 render를 거쳐 테스트하지 말고 로직을 분리해서 unit test (`CreateMCPServer` + `createServerPayload.ts` 참고)
- 사용자가 지각할 수 있는 것을 정확히 assert. `findBy*` 선호, `waitFor` 콜백에는 단일 assertion만
- 절대 전체 unit suite(`npx vitest run`)를 돌리지 말고 변경한 파일만 명시 경로로 실행 (전체 스위트는 380개 파일)
- `eslint --fix`는 testing-library/jest-dom 플러그인에 대해 신뢰 금지 (대량 수정 시 diff 반드시 확인)
- `eslint-suppressions.json`의 grandfathered 위반을 수정하면 `eslint . --prune-suppressions`로 baseline을 축소해서 커밋
- 테스트가 라이브러리 CSS class로 요소를 찾아야 할 때는 role/label/title/ARIA 상태가 없는 경우에만 (그때도 `data-slot` 우선)

## 7. 주요 명령 (litellm-dashboard/ 기준)

```bash
npm run dev             # 개발 서버 (http://localhost:3000)
npm run build           # static export (out/)
npm run lint            # ESLint
npm run test:unit -- <파일>      # unit (필수: 명시 경로)
npm run test:component -- <파일>
npm run test:integration -- <파일>
npm run test:types      # 타입체크
npm run gen:api         # OpenAPI 스펙 → schema.d.ts 재생성
npm run knip            # 무사용 코드 검사
npm run format:check    # Prettier 체크
```
