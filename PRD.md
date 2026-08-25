# LiteLLM OSS 단일 Go Proxy PRD

## 1. 목적

LiteLLM의 독립 Proxy 서버에 필요한 OSS 기능을 Go로 재구현한다. 결과물은 Python SDK를 제공하지 않고, OpenAI 호환 HTTP API를 제공하는 단일 Go 바이너리 `litellm-proxy`다

개발과 배포에서 Python, uv 및 Python 런타임 의존성을 제거해 빌드, 로컬 실행, 배포와 운영을 단순화한다

## 2. 범위

### 포함

- OpenAI 호환 API: `/v1/models`, `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`
- Server-Sent Events 기반 스트리밍, 요청 취소, deadline 전파와 OpenAI 호환 오류 응답
- master key 및 virtual key 인증, 기본 역할 기반 접근 제어, 허용 모델 검증
- 모델 별칭, provider deployment 선택, timeout, retry, fallback과 load balancing
- 1차 provider: OpenAI, Azure OpenAI, Anthropic, AWS Bedrock, Google Gemini 또는 Vertex AI
- PostgreSQL 기반 사용자, 팀, 프로젝트, virtual key, 사용량, 비용 및 예산 데이터
- Redis 기반 cache, rate limit, 분산 lock과 짧은 수명의 조정 상태
- health/readiness, 구조화 로그, Prometheus metrics, OpenTelemetry trace propagation
- 기존 `config.yaml` 및 `.env`의 P0 설정 키 호환
- Go multi-stage Docker 이미지와 단일 `litellm-proxy` 바이너리

### 제외

- Python SDK 및 Python SDK 호환 계층
- `ui/` 및 관리 대시보드의 빌드·배포·재작성
- `enterprise/`의 코드, API, 라이선스 기능과 Go 이식
- SSO, SCIM, Enterprise 감사 로그, 관리형 파일·벡터 스토어와 Enterprise 전용 guardrail
- P1 API: audio, image generation, rerank, batches, realtime/WebSocket, MCP, A2A
- 모든 LiteLLM provider의 즉시 이식

## 3. 제품 원칙

- Proxy 자체는 하나의 Go 바이너리로 실행한다
- 운영 환경의 PostgreSQL과 Redis는 외부 서비스로 유지한다
- 로컬 전환 검증에서만 `--local-dev`가 embedded PostgreSQL과 miniredis를 자동 기동한다
- PostgreSQL은 영속 데이터의 source of truth이며 Redis는 cache와 조정 상태만 보관한다
- 설정에 지원하지 않는 Python 전용 필드가 있으면 시작 시 명확하게 실패하거나 경고한다. 무시해서 동작 의미가 달라져서는 안 된다
- Enterprise 코드는 라이선스 경계를 유지하기 위해 빌드, 바이너리 및 런타임에서 제외한다
- 원본 Python 파일 구조를 기계적으로 복제하지 않는다. 공개 계약과 단일 책임을 기준으로 idiomatic Go 패키지로 재구성한다

## 4. 사용자 시나리오

| 사용자 | 시나리오 | 기대 결과 |
| --- | --- | --- |
| 애플리케이션 개발자 | OpenAI SDK의 base URL을 Proxy로 변경 | OpenAI 형식 요청과 스트리밍 응답이 동작 |
| 플랫폼 운영자 | 하나의 모델 이름에 여러 provider deployment를 구성 | 정책에 맞는 deployment를 선택하고 장애 시 fallback |
| 조직 관리자 | virtual key를 발급하고 모델·예산을 제한 | 모든 요청에 key scope, model allowlist와 limit가 적용 |
| 개발자 | `--local-dev`로 Proxy를 실행 | PostgreSQL과 Redis 설치 없이 로컬에서 Python 대비 검증 |

## 5. 아키텍처

```text
Client / OpenAI SDK
        |
  Go Proxy HTTP Server
        |
Auth -> Rate/Budget -> Router -> Provider Adapter
 |                         |             |
 PostgreSQL + Redis <- Usage/Cost -------+
```

권장 패키지 구조는 다음과 같다

```text
cmd/litellm-proxy/       바이너리 진입점
internal/config/         config.yaml, .env, validation
internal/httpapi/        HTTP handler, middleware, SSE, OpenAI 오류
internal/auth/           master key, virtual key, RBAC
internal/routing/        model registry, retry, fallback, balancing
internal/providers/      provider adapter와 request/response 변환
internal/store/postgres/ PostgreSQL repository와 migration
internal/store/redis/    cache, rate limit, lock
internal/usage/          token usage, price, spend, budget
internal/observability/  log, metrics, trace
internal/localdev/       embedded PostgreSQL과 miniredis lifecycle
```

## 6. 데이터와 로컬 개발

기존 `schema.prisma`의 PostgreSQL 스키마와 식별자 계약을 기준선으로 삼는다. Go 구현은 PostgreSQL에 직접 접근하며 Python Prisma client를 사용하지 않는다. Go migration 도구 또는 검증된 SQL migration 산출물을 채택한다

`--local-dev` 모드는 다음을 수행한다

1. `embedded-postgres`로 실제 PostgreSQL child process를 시작한다
2. 임시 data directory에 migration을 적용한다
3. `miniredis`를 같은 Go 프로세스에서 시작한다
4. 자동으로 구성한 `DATABASE_URL`과 Redis 주소로 Proxy를 실행한다
5. 종료 시 child process와 임시 리소스를 정리한다

embedded PostgreSQL은 실제 단일 파일 내장 DB가 아니다. 플랫폼별 PostgreSQL 실행 파일을 내려받거나 릴리스 아티팩트와 함께 제공해야 한다. 이 모드는 개발·전환 검증 전용이며 운영 모드가 아니다

## 7. 기술 선택

| 관심사 | 선택 |
| --- | --- |
| HTTP/SSE | Go 표준 `net/http` 중심 구현 |
| PostgreSQL | `pgx/v5` |
| Redis | `go-redis/v9` |
| 로컬 PostgreSQL | `fergusstrange/embedded-postgres` |
| 로컬 Redis | `alicebob/miniredis/v2` |
| 설정 | YAML decoder + 환경변수 overlay |
| 인증 | 검증된 Go JWT 라이브러리와 bcrypt 또는 argon2 기반 key hash |
| 관측성 | Prometheus 공식 Go client와 OpenTelemetry Go SDK |

모든 외부 Go 의존성은 `go.mod`에 명시적 버전으로 고정하고, 도입 시 라이선스와 유지보수 상태를 확인한다

## 8. API와 호환성 계약

P0 endpoint는 기존 Python Proxy를 기준으로 다음을 유지한다

- URL, HTTP method, Authorization header와 OpenAI request/response 필드
- JSON error shape, 상태 코드 및 SSE event 순서
- 취소 시 upstream request 취소와 한 번만 기록되는 사용량
- model alias, virtual key의 tenant scope와 model allowlist
- `config.yaml`의 P0 설정 의미

호환성은 문서 검토가 아니라 contract test로 판정한다. 동일 fixture를 Python 기준 Proxy와 Go Proxy에 전송해 상태 코드, 필수 header, JSON 의미, SSE events, DB usage/cost 기록을 비교한다

## 9. 구현 단계

### 단계 0. 기준선과 migration manifest

- P0 endpoint, config key, 환경변수, PostgreSQL table, Redis command를 인벤토리화한다
- Python 요청/응답과 오류 fixture를 language-neutral JSON 또는 YAML로 추출한다
- 각 Python 구현 항목에 Go package, test 및 상태를 연결하는 migration manifest를 만든다
- P0 provider와 provider별 지원 API를 확정한다

완료 기준: P0 동작, 데이터 및 설정 계약이 테스트 가능한 명세로 고정된다

### 단계 1. Go 기반과 로컬 개발

- `go.mod`와 `cmd/litellm-proxy`를 만든다
- 설정 validation, HTTP middleware, health/readiness, graceful shutdown을 구현한다
- `--local-dev`, embedded PostgreSQL, miniredis, migration lifecycle을 구현한다
- PostgreSQL·Redis repository interface와 테스트 fixture를 만든다

완료 기준: 외부 서비스 설치 없이 `litellm-proxy --local-dev --config config.yaml`이 기동한다

### 단계 2. 인증과 데이터 운영

- master key와 virtual key authentication을 구현한다
- 사용자, 팀, 프로젝트, key scope와 model allowlist를 구현한다
- usage, cost, budget 및 rate limit의 최소 데이터 흐름을 구현한다

완료 기준: 유효하지 않은 key와 권한 밖 모델 요청이 일관되게 거부되고, 허용 요청은 사용량을 기록한다

### 단계 3. 핵심 Proxy와 provider

- models, chat completions, responses, embeddings endpoint를 구현한다
- SSE streaming, cancellation, timeout과 OpenAI 오류 변환을 구현한다
- OpenAI provider부터 adapter를 완성하고 나머지 P0 provider를 순차적으로 추가한다
- 모델 registry, retry, fallback과 load balancing을 구현한다

완료 기준: OpenAI SDK가 Go Proxy를 대상으로 P0 endpoint와 streaming을 사용할 수 있다

### 단계 4. 운영 준비

- Redis cache, rate limit, lock 및 budget concurrency를 강화한다
- metrics, traces, logs와 운영 관리 API를 구현한다
- Dockerfile과 entrypoint를 Go runtime으로 교체한다
- Python/uv/Prisma Python runtime을 Proxy 이미지에서 제거한다

완료 기준: 외부 PostgreSQL·Redis를 사용한 컨테이너 배포가 Python 없이 동작한다

### 단계 5. 검증과 전환

- Python Proxy와 Go Proxy의 fixture 및 실제 provider contract test를 실행한다
- 실제 PostgreSQL·Redis 통합 테스트와 부하·장애 테스트를 실행한다
- shadow traffic, canary, rollback 지표를 정의한다
- P0 호환성이 검증된 Python Proxy 실행 경로를 제거한다

완료 기준: 대상 P0 트래픽이 Go 바이너리만으로 운영되고 Python Proxy runtime이 필요 없다

## 10. 테스트 전략

- Unit: routing, auth, config, pricing, error mapping과 provider 변환
- Contract: Python 기준 Proxy와 Go Proxy에 동일 fixture 전송
- Local integration: `--local-dev`의 embedded PostgreSQL + miniredis
- Full integration: 실제 PostgreSQL과 Redis를 대상으로 migration, lock, cache, rate limit, concurrency 검증
- Provider integration: 실제 provider 계정으로 success, streaming, timeout, retry 및 error path 검증
- Load and resilience: provider outage, Redis reconnect, PostgreSQL restart, graceful shutdown 검증

miniredis는 테스트용 구현이므로 Cluster, Sentinel, Lua scripting 또는 영속성에 의존하는 흐름은 실제 Redis 통합 테스트에서 별도로 검증한다

## 11. 수용 기준

- Go 바이너리는 Python 런타임 없이 P0 Proxy를 실행한다
- OpenAI SDK가 P0 endpoint와 streaming을 호출할 수 있다
- virtual key, model allowlist, rate limit과 budget이 provider 호출 전에 적용된다
- usage와 cost가 PostgreSQL에 정확히 한 번 기록된다
- local-dev 모드는 PostgreSQL과 Redis의 사전 설치 없이 동작한다
- 운영 Docker 이미지는 Go binary와 필요한 migration artifact만 포함한다
- Enterprise와 UI 의존성은 Go Proxy 이미지와 바이너리에 포함되지 않는다
- Python 기준 contract suite와 Go 후보가 합의된 P0 호환성 기준을 충족한다

## 12. 리스크와 대응

| 리스크 | 대응 |
| --- | --- |
| 기존 Proxy 표면적이 넓음 | P0 contract를 먼저 고정하고 P1을 명시적으로 제외 |
| PostgreSQL 스키마의 Python Prisma 결합 | schema compatibility matrix와 명시적 Go SQL migration 작성 |
| miniredis와 실제 Redis의 차이 | CI에서 실제 Redis 통합 테스트를 필수화 |
| embedded PostgreSQL의 플랫폼 binary 필요 | local-dev 전용으로 한정하고 플랫폼별 release artifact 제공 |
| provider별 응답 차이 | provider별 golden fixture와 실제 provider integration test 유지 |
| Enterprise 라이선스 경계 | `enterprise/`를 import하거나 이식하지 않고 별도 후속 승인 범위로 유지 |

## 13. 후속 범위

P0 출시 뒤 별도 PRD와 승인으로 P1 API, 추가 provider, 관리 UI 연동, MCP/A2A 및 Enterprise 이식 여부를 평가한다
