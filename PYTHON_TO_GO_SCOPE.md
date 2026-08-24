# Python 코드의 Go 재개발 범위

## 목적

이 문서는 `ui/`와 Enterprise 경계를 제외한 LiteLLM의 Python 코드를 Go로 재개발하기 위한 범위를 정의한다. 최종 목표는 OSS/core 영역에서 Python 런타임·패키지·테스트·CLI·스크립트 없이 Go 서비스, Go SDK, Go CLI, Go test만으로 LiteLLM을 빌드·검증·배포하는 것이다. `ui/`의 대시보드와 Enterprise Python backend는 그대로 유지한다

이 문서는 Python 파일을 줄 단위로 번역하라는 의미가 아니다. Python 구현이 제공하는 공개 계약, provider 동작, 운영 기능, 데이터 의미론을 Go의 단순하고 강타입인 설계로 재구현한다

다만 기능 누락을 막기 위해 구현 순서는 두 단계로 엄격히 분리한다. 먼저 Python 파일의 책임과 테스트를 1:1로 대응하는 Go parity 파일로 재개발한다. 모든 mapping과 Go test가 완료된 뒤, 별도 리팩터링 단계에서만 Go에 맞는 package 통합, 파일 분할, 중복 제거를 수행한다

## 기준과 범위 규칙

2026-08-24 기준 저장소에는 추적된 Python 파일이 5,277개 있다. 이 중 주요 실행 범위는 다음과 같다

| 영역 | 규모 | Go 전환 원칙 |
| --- | ---: | --- |
| `litellm/llms/` | 893 파일 | 모든 지원 provider·endpoint 변환을 Go adapter로 재개발 |
| `litellm/proxy/` | 576 파일, 약 757 route 선언 | OSS/core Gateway, 관리 API, 인증, routing/cache, DB/worker를 Go 서비스로 재개발. Enterprise 경로는 제외 |
| `litellm/integrations/` | 197 파일 | 관측성·callback·prompt/guardrail·외부 서비스 integration을 Go native 또는 webhook contract로 재개발 |
| `enterprise/` | 149 파일 | 현재 제외. Python Enterprise backend와 테스트를 유지 |
| `tests/` | 대규모 Python test suite | UI 전용 TS/Playwright를 제외하고 Go test, Go E2E, Go load test로 재작성 |
| 기타 | SDK, router, cache, secret manager, model/type, CLI, script, cookbook, migration | 유지할 제품 기능은 Go로 재개발하고, 문서 예제·생성물·중복 도구는 삭제 또는 Go 예제로 교체 |

### 포함

- `litellm/`, `litellm-proxy-extras/`, `backend/`, `gateway/`, `scripts/`, `db_scripts/`, `ci_cd/`, `migrations/`, `cookbook/`, `tests/`의 OSS/core Python 실행 코드와 test code
- Python wheel/CLI, FastAPI Proxy, background worker, custom callback/plugin, local mock server, benchmark/load tool
- Python이 생성하거나 검사하는 API schema, config parser, database migration helper, deployment validation logic

### 제외

- `ui/`의 Next.js/React/TypeScript 대시보드 코드와 UI 전용 Playwright 테스트
- `enterprise/`, Enterprise 전용 `litellm/` 경로와 Enterprise test. 이들은 `excluded-enterprise` 상태로 inventory에 기록하고 현재 Go 포팅하지 않는다
- Markdown, YAML, JSON, SQL, HCL, Helm chart, Dockerfile와 같이 실행 언어가 아닌 설정·문서 artifact
- 기능의 source-of-truth가 아닌 generated asset, 오래된 example, 중복 test/tool. 이들은 Go로 포팅하지 않고 단계 0 inventory에서 삭제 근거를 기록한다

## 파일 1:1 parity migration contract

각 Python 파일은 migration manifest에서 아래 중 하나의 종료 상태를 가져야 한다

| 상태 | 적용 대상 | 필수 증거 |
| --- | --- | --- |
| `port` | 실행 동작, public API, test, CLI, script, provider transform | 원본 파일 1개와 Go parity source 1개, Go test, contract fixture, owner |
| `delete-generated` | 빌드 결과물, vendored/generated output | source-of-truth와 재생성 또는 삭제 근거 |
| `delete-duplicate` | 동일 기능의 중복 helper/test/example | 대체 Go path와 behavior가 중복임을 보이는 근거 |
| `retire-feature` | 지원 종료가 승인된 experimental 기능 | 제품 승인, API/문서 제거, migration 안내 |

`port` 상태의 필수 필드는 다음과 같다

```text
source_path: litellm/proxy/auth/user_api_key_auth.py
source_language: python
responsibility: virtual-key authentication and scope resolution
public_contract: proxy authentication behavior and error fixture IDs
go_parity_path: internal/parity/litellm/proxy/auth/user_api_key_auth.go
go_test_path: internal/parity/litellm/proxy/auth/user_api_key_auth_test.go
status: parity-passing
owner: <team or individual>
```

- `go_parity_path`는 원본 경로와 이름을 추적할 수 있어야 한다. Go package 문법상 필요한 이름 조정은 허용하지만 mapping ID 없이 책임을 합치거나 분할할 수 없다
- 원본 Python source 하나는 parity 단계에서 하나의 Go source 책임으로 대응한다. 원본 test 하나도 하나의 Go test file 또는 명시된 Go subtest set으로 대응한다
- Python assertion은 가능한 한 그대로 Go assertion으로 옮기고, request/response fixture는 JSON/YAML로 추출한다
- parity 단계에서는 behavior 변경, 새 feature, Go다운 package 통합을 금지한다. 보안상 긴급 변경은 별도 manifest ID와 regression test로 기록한다
- parity 완료 뒤 refactor를 시작한다. refactor는 migration ID와 Go test를 유지하며, public contract가 바뀌면 별도의 versioned API migration으로 처리한다

## Go 목표 구조

```text
cmd/
  gateway/        OpenAI-compatible HTTP/SSE/WebSocket server
  worker/         usage, spend, audit, webhook, scheduled work
  litellm/        Go CLI
  mock-provider/  Go E2E/local-test provider
  migration/      Go database/config migration tools

sdk/              Public Go SDK
internal/
  api/            HTTP transport, OpenAPI, auth middleware
  routes/         Call-type handlers
  providers/      Provider adapters and request/response transforms
  routing/        Deployments, retries, fallbacks, load balancing
  policy/         RBAC, budget, rate limits, guardrails, policies
  usage/          Token/cost calculation, spend ledger, outbox worker
  storage/        PostgreSQL repositories and migrations
  cache/          Memory and Redis/Valkey implementations
  secrets/        Environment/KMS/secret-manager integrations
  observability/  Logs, metrics, tracing, callback/webhook delivery
```

parity 단계에서는 `internal/parity/` 아래에 원본 책임을 추적하는 Go 파일을 둔다. 이 단계의 구조는 임시이며, parity 완료 뒤 `internal/`의 domain package로 이동한다. 최종 구조에서는 HTTP handler, provider transform, DB repository, callback delivery가 서로 직접 섞이지 않도록 Go의 컴파일 경계로 강제한다

## Python 기능군별 Go 재개발 범위

### 1. Public SDK와 공통 domain, P0

대상: `litellm/main.py`, `litellm/router.py`, `litellm/types/`, `litellm/models/`, `litellm/exceptions.py`, `litellm/constants.py`, `litellm/litellm_core_utils/`, `litellm/endpoints/`

- Go SDK는 completion/chat, responses, embeddings, images, audio, batches, files, vector stores, rerank, search, OCR, realtime, A2A 등 현재 public API를 typed request/response와 `context.Context`로 제공한다
- Python sync/async 이중 API는 Go의 blocking call, iterator/channel stream, context cancellation으로 통합한다
- Python Pydantic/dataclass/dict 타입은 Go struct, enum, validation으로 대체한다. `map[string]any`는 provider-specific escape hatch로만 제한한다
- exception hierarchy는 stable error code, HTTP status, retryability, sanitized upstream detail을 가진 Go error model로 대체한다
- token counting, model cost map, prompt template, response normalization, model capability는 Go domain package가 소유한다

완료 기준: public API별 request/response/streaming/error fixture가 Go SDK와 Go Gateway 양쪽에서 통과하고 Python SDK import/runtime은 제거된다

### 2. Provider adapter와 protocol 변환, P0부터 단계 확장

대상: `litellm/llms/`, `litellm/anthropic_interface/`, `litellm/google_genai/`, `litellm/ocr/`, `litellm/realtime_api/`, `litellm/responses/`, `litellm/images/`, `litellm/files/`, `litellm/batches/`, `litellm/vector_stores/` 등

- 100개 이상 provider와 OpenAI, Anthropic Messages, Responses, Realtime, A2A 등 각 protocol surface를 Go adapter로 구현한다
- adapter는 provider별 request transform, authentication, URL construction, response/stream normalization, usage extraction, error mapping만 담당한다
- 공통 retry, timeout, routing, logging, spend write는 adapter 밖 Go service가 담당한다
- 신규 provider 추가가 기존 flow 복사가 아니라 typed adapter registration과 provider-specific transform으로 끝나도록 설계한다
- Rust에 이미 구현된 messages, responses/realtime, OCR, transcription transform은 `RUST_TO_GO_SCOPE.md`의 mapping과 동일한 Go package로 통합한다

완료 기준: 지원 목록의 provider·endpoint 조합마다 Go contract test 또는 지원 중단 결정이 있고, Python provider module은 남지 않는다

### 3. Gateway와 관리 API, P0

대상: `litellm/proxy/`, `backend/`, `gateway/`, `litellm/proxy_auth/`

- OpenAI-compatible public API, provider passthrough, health, metrics, Swagger/OpenAPI, SSE, WebSocket, file upload 및 cancellation
- API key, JWT, master key, user/team/project/organization scope, RBAC, SSO, SCIM, rate limit, concurrency limit, budget, policy/guardrail
- model deployment config, router, fallback, retry, load balancing, cooldown, health check, request lifecycle
- PostgreSQL repository, Redis/Valkey cache/rate-limit state, usage/spend ledger, audit outbox, background worker, retention/partition task
- dashboard가 소비하는 모든 관리 API와 `/ui/` static asset serving contract

완료 기준: 대시보드와 Terraform provider가 Go Gateway API만 사용해 API key, users/teams/projects, models/routing, policies/guardrails, cost/usage, logs/audit, SSO/SCIM 화면을 E2E로 통과한다

### 4. Routing, cache, secret, configuration, P0

대상: `litellm/router_strategy/`, `litellm/router_utils/`, `litellm/caching/`, `litellm/secret_managers/`, `litellm/repositories/`, `litellm/proxy/config_resolvers/`, `litellm/proxy/example_config_yaml/`

- YAML config parser, schema validation, environment/secret reference resolution, hot reload/rollout state
- deployment selection, weighted routing, fallbacks, retries, cooldown, health-aware routing, budget/complexity/adaptive strategies
- memory, Redis/Valkey, semantic, object-store cache implementations과 cache invalidation
- cloud secret manager/KMS integration 및 credential lifecycle
- Python Prisma/client wrapper 의존 없이 Go repository와 explicit transaction/outbox boundary 구현

완료 기준: Go가 Python interpreter 없이 기존 지원 YAML의 inventory된 의미를 파싱하고, multi-instance routing/cache/budget 동작을 재현한다

### 5. Observability, callback, guardrail, P0

대상: `litellm/integrations/`, `litellm/proxy/hooks/`, `litellm/proxy/guardrails/`

- OpenTelemetry, Prometheus, structured logging, tracing, request/response logging, alerting, webhook/callback delivery
- custom logger, custom guardrail, content safety, policy inheritance, prompt management, spend/cost reporting
- 기존 Python class plugin은 Go interface를 binary plugin으로 노출하지 않는다. Go native plugin contract 또는 versioned external webhook/event contract로 재설계한다

완료 기준: callback/spend/audit이 Go worker와 PostgreSQL outbox로 동작하며, Python callback runtime이나 Python Proxy callback endpoint가 필요하지 않다

### 6. 제품 endpoint와 확장 기능, P1

대상: `litellm/a2a_protocol/`, `litellm/assistants/`, `litellm/interactions/`, `litellm/rag/`, `litellm/sandbox/`, `litellm/search/`, `litellm/skills/`, `litellm/evals/`, `litellm/compression/`, `litellm/passthrough/`, `litellm/fine_tuning/`, `litellm/videos/`

- 각 기능은 endpoint contract, persistence, provider support, dashboard impact, security boundary, test fixture를 inventory에 등록한 뒤 Go package로 구현한다
- MCP, A2A, sandbox, RAG, assistant, vector/file 기능은 별도 process 또는 service가 필요한지 RFC에서 결정한다. 구현 언어는 Go이며 Python sidecar를 허용하지 않는다
- experimental 기능은 supported, Go로 재개발, 삭제 중 하나를 결정한다. “Python 코드 유지”는 선택지가 아니다

### 7. CLI, 테스트, 도구, 예제, P0부터 단계 확장

대상: `litellm/proxy/client/cli/`, `scripts/`, `db_scripts/`, `ci_cd/`, `migrations/`, `cookbook/`, `tests/`, Python 기반 mock/benchmark/load tool

- Python CLI는 `cmd/litellm` Go CLI로 대체하며, login/SSO polling, local proxy, key/user/team/model 관리, agent integration을 지원한다
- Python unit/integration/E2E/load test는 `go test`, Go test server, Go benchmark/load harness로 재작성한다
- API golden fixture는 JSON/YAML로 보존하고 language-specific test helper는 Go test helper로 재작성한다
- migration, data repair, schema validation, benchmark, CI gate는 Go binary 또는 Go test로 대체한다
- cookbook 예제는 지원하는 사용자 흐름이면 Go example으로 전환하고, 제품 계약을 검증하지 않는 임시·중복 예제는 삭제한다

완료 기준: 대시보드 UI 테스트 외에 `pytest`, Python mock server, Python CLI, Python-based CI gate를 호출하는 build/test/release path가 없다

## 포팅 우선순위

| 단계 | Go 재개발 범위 | 종료 기준 |
| --- | --- | --- |
| 0. Inventory | 모든 Python 파일을 `port`, generated, duplicate, retire로 분류하고 1:1 manifest 생성 | source path, responsibility, public contract, Go parity source/test, owner, status가 기록 |
| 1. Foundation parity | config, errors, types, auth, storage, cache, telemetry, test fixture framework의 1:1 Go 구현 | Go Gateway가 PostgreSQL/Redis 선택 모드에서 기동하고 원본 test assertion 대응 |
| 2. Core gateway parity | chat/responses/embeddings, OpenAI/Anthropic/Azure/Bedrock/Gemini, routing, usage의 1:1 Go 구현 | P0 API contract와 streaming, Python test 대응을 Go-only로 검증 |
| 3. OSS/core parity | 나머지 provider, endpoint, integration, cache, secret, extension, SDK/CLI/tooling/test | `port` 상태의 모든 OSS/core Python source에 Go parity source/test가 있고, 기능 누락이 없음 |
| 5. Refactor | Go package 통합, shared abstraction, file split/merge, duplicate removal | refactor 전후 Go contract/E2E/load test 통과, public behavior 불변 |
| 6. Removal | OSS/core Python package, dependencies, images, CI, packaging 제거 | `ui/`와 Enterprise 경계 밖 Python source와 Python runtime 의존이 0 |

## 최종 검증 게이트

- `git ls-files` 기준 `ui/`와 Enterprise 경계 밖에 Python source가 없다. 예외가 필요한 데이터 fixture는 `.py`가 아닌 JSON/YAML/SQL로 변환한다
- production image, Go SDK/CLI image, Go test/tool image에 Python interpreter, `pip`, Python wheel, PyO3 bridge가 없다
- 모든 지원 provider/endpoint에는 Go implementation, contract test, owner가 있다
- 모든 `port` Python 파일에는 source-to-Go parity mapping, Go parity test, parity-passing 상태가 있다. refactor는 이 gate 뒤에만 시작하고, refactor 전후 mapping ID와 contract fixture를 유지한다
- OSS/core 기능과 해당 dashboard 화면이 Go backend와 PostgreSQL/Redis 또는 standalone 실행 모드에서 문서화된 방식으로 동작한다. Enterprise 화면은 Python Enterprise backend를 유지한다
- CI는 Go build, Go test, Go E2E/load test, dashboard UI test만 실행하고 `pytest` 또는 Python script에 의존하지 않는다
- compatibility proxy는 종료 전에 제거하며, final Go-only canary가 운영 SLO를 충족해야 한다
