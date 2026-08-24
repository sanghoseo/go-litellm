# LiteLLM 비-UI 전체 Go 재개발 및 Enterprise 기능 PRD

## 1. 문서 목적

이 문서는 LiteLLM의 대시보드 외 모든 실행 코드와 테스트를 Go로 재개발하고, 기존 Next.js 대시보드는 유지하며, Enterprise 기능을 Go 플랫폼에 구현하기 위한 제품 범위, 우선순위, 호환성 기준 및 요구사항을 정의한다. 최종 제품 저장소에는 대시보드 이외의 Python·Rust·비대시보드 JavaScript/TypeScript 소스가 남지 않는다

## 2. 현황 및 근거

2026-08-20 저장소 조사 기준:

- 추적된 Python 파일은 5,277개, Rust 파일은 110개다. Enterprise 하위에는 Python 파일 149개가 있다
- 대시보드 밖 JavaScript/TypeScript 파일은 296개가 있으나, 이 중 상당수는 experimental 경로의 생성된 정적 asset이다. 단계 0에서 source, generated artifact, 삭제 대상으로 구분한다
- Python Proxy에는 약 757개의 HTTP 라우트 선언이 있으며, 관리, 인증, 비용, 관측성, OpenAI 호환 API 등 넓은 표면적을 가진다
- `litellm/llms/`에는 100개가 넘는 공급자 어댑터가 있다
- Rust 워크스페이스는 `core`, `ai-gateway`, `python-bridge` 크레이트로 분리되어 있고, messages, responses, audio transcription, OCR, realtime 및 일부 OpenAI, Anthropic, Bedrock, Azure AI, Vertex AI, Mistral 경로를 단계적으로 구현 중이다. 구성, 재시도, 라우팅 정책, 로깅, 비용 추적, 고객 플러그인은 여전히 Python이 담당한다. Rust 런타임도 최종적으로 Go 구현으로 대체 대상이다
- Enterprise 코드는 상업 라이선스 경계 안에 있으며, 감사 로그, 키 및 프로젝트 관리, 내부 사용자 관리, SSO 확장, 관리형 파일·벡터 스토어, 알림 콜백 등의 기능이 존재한다
- 현 저장소의 Go 코드는 Terraform provider와 예제 수준으로, 실행 가능한 Go Gateway 모듈은 없다
- 주 대시보드는 `ui/litellm-dashboard/`에 있다. Next.js 16.2, React 19.2, TypeScript 5.9, Tailwind CSS 4, shadcn/ui, TanStack Query/Table, OpenAPI 타입 생성, Vitest로 개발된다. `enterprise/enterprise_ui/`는 현재 주 대시보드 구현체가 아니다

결론적으로 본 프로젝트는 단순 언어 변환이 아니다. 데이터 계약, OpenAI 호환 API, 공급자별 변환, 인증·권한, 비용 정산 및 운영 기능을 보존하는 플랫폼 마이그레이션이다

## 3. 제품 비전

Go 기반 LiteLLM 플랫폼을 비-UI 단일 구현 언어로 제공한다. Go 서비스와 Go SDK는 현재 Python SDK·Proxy·관리 API·CLI·백그라운드 작업·테스트·Rust core/bridge/gateway가 제공하는 동작을 모두 대체한다. 기존 Next.js 대시보드는 유일한 UI 예외로 유지하며, Go API의 호환 계약을 통해 같은 기능을 계속 제공한다. Enterprise 고객은 중앙 인증, 세분화된 권한, 정책 집행, 비용 통제 및 감사를 기본 기능으로 사용해야 한다

## 4. 목표와 비목표

### 목표

1. `ui/litellm-dashboard/`를 제외한 모든 실행 코드와 테스트를 Go 구현 또는 비코드 설정으로 대체한다
2. 기존 Python Proxy와 Rust 서비스를 병행 실행하면서 요청 단위로 안전하게 Go로 전환한다
3. 모든 지원 endpoint와 공급자에 대해 요청·응답·스트리밍·오류 의미론의 호환성을 보장한다
4. Enterprise 핵심 제어면을 Go 서비스에 포함한다: 조직·프로젝트·팀·사용자·가상 키, RBAC, SSO, SCIM, 예산, 감사 로그 및 정책 기반 Guardrail 적용
5. 현재 Next.js 대시보드와 그 TypeScript UI 테스트를 유지하고, 대시보드가 소비하는 API·인증·정적 asset 배포 계약을 보장한다
6. 운영 데이터의 PostgreSQL 및 Redis 계약을 명시하고, 기존 배포·Terraform·관리 UI와 연동한다
7. 새 Enterprise 기능으로 정책-as-code 배포, 불변 감사 내보내기, 예산 임계치 자동 조치를 제공한다

### 비목표

- 대시보드를 Go 또는 다른 프런트엔드 기술로 재작성하지 않는다. 현재 Next.js/React/TypeScript 대시보드를 유지한다
- 현재 Rust 코드의 기계적 포팅을 목표로 하지 않는다. 검증된 동작과 공개 계약을 기준으로 Go 설계를 새로 만든다
- 최종 상태에서 `ui/litellm-dashboard/` 밖의 Python, Rust, JavaScript, TypeScript 실행 소스를 요청 처리, SDK, CLI, worker, provider adapter, plugin 또는 테스트에 요구하지 않는다
- YAML, JSON, SQL, HCL, Dockerfile, Helm chart, Markdown 및 대시보드의 TypeScript/TSX는 Go 코드 전환 대상이 아니다. 기존 Python/Rust/비대시보드 JS/TS의 동작은 Go로 재구현하거나 생성물·중복물은 삭제한다
- Enterprise 라이선스 코드를 오픈소스 경로로 복사하거나 상업 라이선스 경계를 변경하지 않는다

## 5. 사용자와 핵심 시나리오

| 사용자 | 시나리오 | 기대 결과 |
| --- | --- | --- |
| 애플리케이션 개발자 | OpenAI SDK의 base URL만 Gateway로 변경 | `/v1/chat/completions`, `/v1/responses`, embeddings를 동일한 인증·스트리밍 방식으로 호출 |
| 플랫폼 운영자 | 모델 장애 또는 비용 조건에 따라 여러 배포를 운영 | 모델 별칭, fallback, 재시도, rate limit, circuit breaking 및 사용량 기록이 일관되게 동작 |
| 조직 관리자 | 조직·프로젝트·팀·사용자 및 가상 키를 관리 | 권한 범위와 모델·예산 제한이 모든 요청 경로에서 강제 |
| 보안 관리자 | SSO/SCIM으로 인력을 관리하고 정책 위반을 조사 | 사용자·그룹 동기화, 감사 가능한 변경 이력, 정책 위반 및 키 사용 기록 확보 |
| 재무 관리자 | 팀과 프로젝트의 LLM 지출을 제어 | 실시간 또는 정의된 지연 한도 내 비용 집계, 한도 차단 및 알림 |

## 6. 제품 범위

### 6.1 최종 전환 범위

최종 전환 범위는 `ui/litellm-dashboard/`를 제외한 실행 코드 전체다. `litellm/`, `litellm-rust/`, `enterprise/`, `backend/`, `gateway/`, `litellm-proxy-extras/`, `scripts/`, `cookbook/`, `tests/`에 있는 Python·Rust·비대시보드 JS/TS 코드는 Go로 재구현하거나 삭제한다. 다음 항목을 포함한다:

- Python SDK의 동기/비동기 public API, typed model, configuration, retry, callback, caching, router, secret manager, provider transform 및 모든 지원 endpoint
- Proxy의 공개/관리/Enterprise API, 인증·권한, policy, guardrail, spend tracking, DB transaction queue, health, background job, webhook 및 observability 경로
- Rust `core`, `ai-gateway`, `python-bridge`의 route, provider, routing, realtime 및 bridge 기능
- 상업 라이선스 경계 안의 Enterprise 런타임 기능. 라이선스 경계 자체는 유지한다
- Python/Rust unit·integration·E2E·load test, local mock server, CLI, migration/maintenance script. UI 전용 Playwright/TypeScript 테스트는 대시보드 유지 범위에 속한다
- `litellm/proxy/_experimental/out/`처럼 생성된 비대시보드 JavaScript bundle은 Go로 포팅하지 않고 source-of-truth를 확인한 뒤 재생성하거나 삭제한다

대시보드와 UI 전용 테스트는 유지 대상이다. Terraform provider는 이미 Go로 구현되어 있어 새 Go API와의 호환성 검증·필요 시 수정 대상이다. Markdown, Helm, Terraform HCL, SQL migration, Docker/CI 설정은 실행 언어 전환 대상이 아닌 배포·운영 artifact로 유지한다

### 6.2 Gateway 데이터면, P0

- OpenAI 호환 API: health/readiness, models, chat completions, responses, embeddings
- SSE 스트리밍, 요청 취소, context deadline 전파, 표준 오류 응답
- 공급자 어댑터 1차: OpenAI/Azure OpenAI, Anthropic, AWS Bedrock, Google Vertex AI/Gemini
- 인증: master key, 가상 키, JWT 기반 세션 토큰. 키의 조직·프로젝트·팀·사용자 범위 및 허용 모델 검증
- 라우팅: 모델 별칭, 가중치 분배, 우선순위, fallback, timeout, 재시도, circuit breaker
- 사용량 및 비용: 토큰 사용량 추출, 가격표 기반 비용 계산, 요청 로그, 일별·월별 집계
- Redis 기반 rate limit, 캐시 및 분산 조정. PostgreSQL 영속화
- 구조화 로그, Prometheus metrics, OpenTelemetry trace propagation 및 감사 이벤트 발행

### 6.3 Gateway 확장, P1

- Anthropic Messages 호환 API, audio transcription, image generation, rerank, batches, realtime/WebSocket
- 공급자 어댑터 2차: Mistral, Cohere, Groq, Ollama/vLLM 및 OpenAI-compatible endpoint
- 요청/응답 Guardrail 훅과 정책 상속
- semantic cache, prompt 관리, 파일·벡터 스토어 프록시, MCP/A2A는 각각 독립 RFC 승인 뒤 추가
- Python/Rust에서 아직 포팅되지 않은 경로는 전환 기간에만 compatibility proxy 모드로 위임한다. 이 위임은 관측 가능하고 설정으로 켜야 하며, 최종 출시 게이트 전에 source와 runtime을 함께 제거한다

### 6.4 Enterprise 제어면, P0

- 테넌시: organization, project, team, user, service account, virtual key의 명확한 소유 관계
- RBAC: system admin, organization admin, project admin, team admin, developer, auditor, viewer 역할과 최소 권한 API
- SSO: OIDC 우선 지원. SAML은 설계가 고정된 뒤 P1로 추가. JIT provisioning과 claim-to-role mapping 지원
- SCIM 2.0: Users/Groups CRUD, pagination, filter, PATCH, deprovisioning, idempotency 및 기존 team/user 모델 매핑
- 예산: 조직·프로젝트·팀·사용자·키 단위 한도, 모델 허용 목록, 기간, soft/hard threshold, 동시성 및 rate limit
- 감사: 인증, 권한 거부, 관리 변경, 키 수명주기, 정책 변경, 모델 라우팅 결정, 비용 한도 조치 기록
- 정책 엔진: 대상(조직·프로젝트·팀·키·모델), 상속, 우선순위, guardrail 및 라우팅 규칙 결합

### 6.5 신규 Enterprise 기능, P1

- 정책-as-code: Git 또는 API로 versioned policy bundle을 검증·승인·배포·롤백하고 적용 버전을 요청 및 감사 이벤트에 기록
- 불변 감사 내보내기: 고객 소유 object storage 또는 SIEM으로 순서 보장된 서명 이벤트 배치를 내보내고 체크포인트로 누락과 변조를 탐지
- 예산 자동 조치: soft threshold 알림, hard threshold 차단, fallback model 전환, 제한 완화 승인 흐름. 조치마다 정책·주체·근거를 감사 로그에 남김

## 7. 대시보드 유지 및 API 계약

대시보드 소스는 `ui/litellm-dashboard/`이며, Next.js static export(`output: "export"`)로 빌드된다. Go 전환은 이 프로젝트를 변경 대상이 아닌 호환성 소비자로 취급한다

- Next.js, React, TypeScript, Tailwind CSS, shadcn/ui 및 현재 대시보드의 빌드·테스트 체인을 유지한다
- 대시보드가 호출하는 API는 `src/lib/http/schema.d.ts`의 OpenAPI 타입과 일치해야 한다. Go API 변경 뒤에는 `npm run gen:api`로 타입을 재생성하고 대시보드 타입 검사·테스트를 통과해야 한다
- same-origin과 `NEXT_PUBLIC_BASE_URL` 기반 split-origin 배포, `/ui/` 경로, 정적 asset prefix, 로그인·SSO redirect 및 httpOnly cookie 보안 모델을 유지한다
- UI 기능별 API parity를 별도 matrix로 관리한다. API 키, 사용자·팀·조직·프로젝트, 모델·라우팅, 사용량·비용, logs/audit, guardrail, policy, SCIM/SSO, MCP/vector store 화면이 포함 대상이다
- 기존 API의 Python-특화 오류 문자열에 UI가 의존하는 부분은 Go의 구조화된 오류 계약으로 대체하되, 대시보드 변경과 backend 변경을 같은 compatibility release에 포함한다

## 8. 호환성 계약

Go Gateway는 다음 계약을 Python Proxy 기준으로 유지한다

- 공개 엔드포인트, HTTP method, 인증 header, OpenAI 형식의 request/response 및 SSE event 순서
- 상태 코드와 오류 형식. 호환 불가능한 개선은 versioned endpoint 또는 명시적 migration flag로 제공
- virtual key, user, team, project, budget 및 spend log의 식별자와 소유 관계
- 기존 YAML 구성의 P0 영역. Go 전용 설정은 별도 namespace 아래 두고, 지원하지 않는 필드는 시작 시 오류 또는 명확한 경고를 낸다
- Helm chart, Docker 환경변수, Terraform provider가 필요한 관리 API 계약

호환성 기준은 문서 비교가 아니라 contract test로 판정한다. 동일한 입력을 Python 기준 인스턴스와 Go 후보 인스턴스에 보내어 상태 코드, 필수 header, JSON 의미론, SSE 이벤트, 비용·사용량 기록, 감사 이벤트를 비교한다

## 9. 아키텍처 원칙

```text
Client / OpenAI SDK / Admin UI
            |
      Go Gateway API
            |
 AuthN/AuthZ -> Policy -> Rate/Budget -> Router -> Provider Adapter
            |                                |             |
 PostgreSQL + Redis <--- Usage/Audit/Events --+        LLM providers
            |
 Enterprise control plane, SSO/SCIM, policy deployment, audit export
```

- 단일 Go module에서 시작하되, `cmd/gateway`, `cmd/worker`, `cmd/sdk-codegen`과 `internal/` 아래 transport, auth, policy, routing, providers, usage, audit, admin, jobs, callbacks, secrets 모듈을 분리한다
- provider adapter는 공통 인터페이스와 강타입 request/response 모델을 사용한다. HTTP handler가 공급자 변환이나 영속화를 직접 수행하지 않는다
- 인증, 권한, 예산, 정책 판정은 provider 호출 전에 완료된다. 비용 확정과 감사 기록은 요청 종료 시 실패 격리된 outbox를 통해 처리한다
- PostgreSQL은 source of truth, Redis는 TTL 상태, rate limit, cache 및 저지연 카운터 용도로만 사용한다
- 관리 변경과 사용량 업데이트는 idempotency key와 transactional outbox를 사용한다
- Enterprise 전용 코드는 명확한 라이선스 패키지 및 빌드 배포 경계에 둔다. OSS 바이너리는 Enterprise 권한 우회를 포함하지 않는다
- 기존 Python SDK는 Go SDK로 대체한다. Go SDK는 별도 Go module로 배포하되, Go Gateway의 domain type과 transport contract를 공유한다. Python SDK 호환 계층이나 generated Python client는 최종 제품 범위에 포함하지 않는다

## 10. 마이그레이션 단계 및 완료 기준

| 단계 | 제공 범위 | 완료 기준 |
| --- | --- | --- |
| 0. 기준선 | 비-UI source inventory, API/데이터 모델 매핑, language-neutral golden fixture, 성능·오류 기준 수집 | 모든 비-UI `.py`, `.rs`, `.js`, `.ts`, `.tsx` 파일이 Go 포팅, 삭제, 생성물 중 하나로 분류되고 P0 endpoint와 Enterprise API의 owner·요청 샘플·DB 영향·지원 여부가 기록 |
| 1. 기반 | Go module, config, HTTP/SSE, PostgreSQL, Redis, telemetry, auth skeleton | health와 가상 키 인증, trace 및 구조화 로그가 배포 환경에서 동작 |
| 2. P0 데이터면 | chat/responses/embeddings, 1차 provider, routing, usage/spend | contract suite 통과 및 shadow traffic 비교에서 허용 불일치 없음 |
| 3. Enterprise P0 | tenancy/RBAC, SSO, SCIM, budgets, audit, policy | 권한 상승, tenant 경계, deprovisioning, budget race에 대한 보안·통합 테스트 통과 |
| 4. 점진 전환 | shadow, canary, request-level rollback, compatibility proxy | 운영 대시보드에서 Python/Go 비교가 가능하고 tenant별 rollback이 즉시 가능 |
| 5. 전체 기능 포팅 | P1 endpoint, 나머지 provider, Go SDK, callbacks/plugins, CLI, background job, Rust-only path 및 비-UI test/tooling | 비-UI inventory의 모든 실행 항목에 Go 구현, contract test, owner 및 제거 상태 기록 |
| 6. 대시보드 통합 | Go OpenAPI, UI API matrix, UI E2E 및 static asset 배포 | 현재 대시보드의 모든 화면이 Go API와 동작하고, generated API type 및 UI E2E 통과 |
| 7. 최종 전환 | 비-UI Python/Rust/JS/TS 제거, 신규 Enterprise 기능 | 모든 production traffic과 비-UI test/tooling이 Go만 사용하며 Python/Rust runtime, bridge, compatibility proxy, 비대시보드 JS/TS source 제거 |

## 11. 기능 요구사항과 수용 기준

### Gateway

- P0 endpoint는 인증된 요청에 대해 OpenAI SDK와 호환되는 JSON 및 SSE를 반환해야 한다
- 스트리밍 요청이 취소되면 provider 요청도 취소하고, 완료된 사용량만 한 번 기록해야 한다
- 라우팅은 구성된 모델 정책을 넘는 provider나 credential을 선택해서는 안 된다
- 재시도는 idempotent하거나 명시적으로 재시도 가능한 요청만 수행하며, 각 시도와 최종 결과를 추적 가능해야 한다
- 비용·예산 판정은 tenant scope를 혼합하지 않아야 하며, hard budget을 초과하는 경쟁 요청은 원자적으로 거부되어야 한다

### Enterprise

- 모든 관리 API는 인증된 주체와 tenant scope를 확인하고, 감사 이벤트를 남겨야 한다
- SCIM deprovisioning 이후 사용자의 세션과 새 요청 권한은 설정된 전파 시간 안에 무효화되어야 한다
- RBAC 테스트는 역할별 허용·거부 매트릭스와 교차 tenant 접근 거부를 포함해야 한다
- 정책 bundle은 schema, 참조 대상, 상속 순환, 충돌 규칙을 검증하지 못하면 배포되지 않아야 한다
- 감사 export는 재시도해도 중복 없이 소비할 수 있고, 순번 gap 또는 서명 불일치를 탐지해야 한다

### 전체 언어 전환 및 대시보드

- 비-UI inventory의 각 실행 항목은 Go 구현 또는 삭제 근거, contract test, 부하 테스트 결과, 문서 및 원본 제거 변경을 연결해야 한다
- 최종 production image와 Go test/tool image에는 Python runtime, Rust binary/toolchain, PyO3 bridge, Python compatibility proxy, Node.js runtime이 포함되지 않아야 한다. 단, 대시보드 build/serve image는 Node.js를 유지한다
- 대시보드는 생성된 OpenAPI 타입과 Go server의 spec이 일치해야 하며, 지원 화면의 E2E test가 Go deployment를 대상으로 통과해야 한다
- 대시보드가 저장하는 인증 정보는 현재 보안 지침대로 `localStorage`에 저장하지 않아야 한다

## 12. 비기능 요구사항

- 보안: TLS 종료 뒤에도 trusted proxy 경계를 검증하고, secret은 로그·감사 payload·오류에 기록하지 않는다. 키는 해시 또는 KMS envelope encryption으로 저장한다
- 신뢰성: provider, Redis, exporter 실패가 인증·정책·감사 데이터 정합성을 우회하지 않아야 한다. 명시된 fail-open/fail-closed 정책을 endpoint별로 둔다
- 성능: P0 기준선은 기존 Python Proxy와 같은 인프라와 공급자 조건에서 P95 Gateway overhead와 오류율을 비교해, 사전에 합의한 SLO를 충족해야 한다. 정확한 수치는 단계 0 측정 후 결정한다
- 관측성: request ID, tenant ID의 비식별 참조, model, route, provider, retry, latency, token/cost outcome을 correlation 가능하게 기록한다
- 운영성: config validation, readiness, graceful shutdown, migration 상태 확인, metric/trace/log export, tenant별 canary와 rollback을 제공한다

## 13. 데이터 및 API 전환 원칙

- 기존 Prisma/PostgreSQL 스키마를 조사해 변경 전에는 schema compatibility matrix와 migration plan을 만든다
- Go는 기존 테이블을 직접 쓰더라도 ORM 모델을 진실의 원천으로 만들지 않는다. 쿼리와 transaction 경계를 명시적으로 소유한다
- 사용량, spend log, audit event는 append-only 우선이며 집계 테이블은 재생성 가능해야 한다
- API 삭제·필드 의미 변경은 금지한다. 새 필드는 additive하게 도입하고 client capability를 확인할 수 없는 변경은 versioning한다

## 14. 테스트 및 출시 게이트

- API contract: 현재 Python/Rust 기준과 Go 후보 간 language-neutral fixture 및 실제 provider sandbox/계정 기반 비교. 기존 Python/Rust test는 Go test로 재작성한다
- 통합: PostgreSQL, Redis, OIDC test provider, SCIM client, object storage/SIEM exporter, 1차 provider별 성공·오류·timeout·streaming 검증
- 보안: tenant isolation, RBAC, key rotation/revocation, forged JWT, SCIM replay, budget concurrency, policy bypass, audit tampering 검증
- 회귀: 현재 Next.js 관리 대시보드와 Terraform provider의 전체 지원 흐름을 Go API에 대한 E2E로 검증
- 성능·복구: sustained load, provider outage, Redis 재시작, DB failover, exporter backlog, graceful shutdown
- 출시: shadow -> 내부 canary -> 선택 tenant canary -> 기본 전환 순서로 진행하며, 오류율·지연·비용 차이·권한 거부율에 자동 rollback threshold를 설정

## 15. 리스크와 결정 필요 사항

| 리스크 | 대응 |
| --- | --- |
| 비-UI 전체 코드 전환의 범위와 누락 | inventory를 source path, 언어, public contract, Go owner, test, 삭제 상태까지 추적하고, 미분류 실행 경로는 완료로 선언하지 않음 |
| Rust 작업과 Go 전환의 중복 투자 | Rust를 동작 및 fixture의 참고 구현으로 분류하고, 새 Gateway 기능의 단일 소유자를 Go로 지정 |
| Python callback/plugin 및 test/tool 의존 | Go native callback/plugin contract, Go CLI, Go test 또는 외부 webhook/event contract로 재구현하고, compatibility proxy는 임시 전환 수단으로만 사용 |
| 비용/예산의 경쟁 조건 | DB 트랜잭션 또는 원자 카운터, idempotency, reconciliation job 및 보수적 hard-limit 정책 적용 |
| Enterprise 라이선스 혼합 | OSS/Enterprise package, 빌드 산출물, CI 및 배포 권한을 분리하고 법무·제품 승인을 거침 |
| 기존 DB 의미론 불명확 | 코드 포팅 전 schema 및 운영 데이터 샘플 기반의 compatibility matrix를 승인 |

착수 전 제품 책임자가 결정해야 할 사항은 다음과 같다: Go 전환의 최초 운영 대상이 self-hosted Gateway만인지 hosted control plane까지인지, P0에서 반드시 지원할 공급자와 endpoint, 기존 Python SDK의 지원 종료·Go SDK 도입 정책, 기존 Enterprise 계약과 신규 기능의 배포·라이선스 정책, compatibility proxy 종료 기한, 그리고 단계 0에서 확정할 성능·가용성 SLO다

## 16. 산출물

- Go Gateway/worker/SDK, container/Helm 배포물 및 운영 runbook
- API·설정·DB compatibility matrix와 자동 contract test suite
- [Rust 구현 범위와 Go 전환 설계](./RUST_TO_GO_SCOPE.md), 비-UI 코드 inventory 및 제거 상태표
- P0 Enterprise control plane, migration 및 rollback 도구
- 정책-as-code, 감사 export, 예산 자동 조치에 대한 RFC와 구현·보안 테스트
- 비-UI Python·Rust·JS/TS 경로별 Go 재개발/삭제 상태표, 제거 계획 및 Go SDK 도입 guide
