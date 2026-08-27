# OSS Python → Go Migration Manifest

## 기준

- 기준 커밋의 대상은 `litellm/` 2,229개와 `litellm-proxy-extras/` 7개 Python 파일이다.
- `enterprise/`, `ui/`, 배포된 wheel·tarball 및 Python test fixture는 이식 대상이 아니다.
- Go는 Python 파일명을 그대로 사용하지 않는다. Go가 `_`로 시작하는 파일을 무시하고, 단일 책임 패키지 구성이 필요하기 때문이다.
- 모든 행은 원본 Python contract, Go 구현, Go unit test, Python 대비 contract test가 모두 완료되어야 완료다.
- 2026-08-27: P0 전환 완료 기준으로 원본 Python 런타임(`litellm/**/*.py`), Python packaging(`pyproject.toml`, `uv.lock`), Python test·CI·빌드 자산(`tests/`, `litellm-proxy-extras/`, `migrations/`, `docker/` Python entrypoint, Python 전용 GitHub workflows·scripts)을 제거했다. 보존한 `litellm/`은 Go 공통 SDK 파일(`context.go`, `errors.go`, `uuid.go` + 테스트)만 남는다. `terraform/`(독립 Go 모듈)·`litellm-rust/`(별도 Rust 트랙)·`helm/`·`db_scripts/*.sql`·`schema.prisma`·`model_prices_and_context_window.json`(Go code-gen 소스)은 Go proxy와 무관하거나 Go가 사용하므로 유지한다.

| 단계 | 원본 영역 | Go 목적지 | 상태 |
| --- | --- | --- | --- |
| A | `litellm/_uuid.py`, `exceptions.py`, `_internal_context.py` | `litellm/` 공통 SDK | P0 error 타입·UUID·context 이식; 나머지 exception 종류는 P0 범위 밖으로 원본과 함께 삭제 |
| B | `litellm/types/`, `types/` | `pkg/types/` | chat/embedding/responses 핵심 OpenAI 타입 이식 |
| C | `litellm/main.py`, `utils.py`, `router.py`, router strategy | `pkg/litellm/`, `internal/routing/` | 공개 chat/text Completion·Embedding·Responses·Moderation·Rerank·Image generation·Speech·Files·Batches/SSE SDK·router retry/alias/fallback·fallbacks 설정 기반 크로스 그룹 폴백·가중치 라우팅·max_fallbacks 이식 |
| D | `litellm/llms/**`, provider transformations | `internal/providers/` | adapter registry·OpenAI·Azure·Anthropic chat/SSE·Gemini chat/SSE/embedding·Bedrock Converse chat/SSE·Cohere v2 chat/SSE/embedding/rerank·OpenRouter 및 `api_base` 기반 OpenAI-compatible(Groq/Mistral/DeepSeek/Perplexity/Together/Cerebras/Ollama/vLLM/xAI/Fireworks/SambaNova/NVIDIA NIM/Anyscale/Databricks) API 이식 |
| E | `litellm/proxy/**` | `internal/httpapi/`, `internal/auth/`, `internal/store/` | PostgreSQL·Redis cache·chat/text/response/embedding/moderation usage·spend·RPM·readiness·master-key 기반 virtual key·team·user·project·organization 생성/조회/목록/부분수정/차단·해제/삭제 및 user/team/project/organization/key 모델 allowlist 교차 검증, 모델 비용 계산·spend 기록·예산 초과 429 강제 집행·SSE 스트리밍 응답 usage/cost 기록 이식 |
| F | `litellm/integrations/**`, logging, callbacks | `internal/observability/`, `internal/integrations/` | Prometheus HTTP metrics·request ID 전파·W3C traceparent 전파(general_settings.forward_traceparent_to_llm_provider) 이식 |
| G | files, batches, audio, images, embeddings, vector stores | `internal/httpapi/`, `internal/providers/` | embeddings·rerank·moderations·OpenAI-compatible image generation/edit, audio/files/batches/vector stores API pass-through 이식; LiteLLM 관리형 multi-provider vector-store registry 대기 |
| H | `litellm-proxy-extras/**`, `schema.prisma` | `internal/store/postgres/`, SQL migrations | `replica_identity.py`는 `migration.go`의 `ApplyReplicaIdentityFull`로 이식; `prisma_toolchain.py`·`utils.py`의 Prisma/Node 부트스트랩과 DB 매니저는 단일 Go 바이너리 범위에서 제거; 핵심 운영 DDL 이식 |
| I | `docker/**`, `pyproject.toml`, `uv.lock`, `.env`, config | Dockerfile, `go.mod`, config loader, SQL migration | 루트 config.yaml·timeout/retry/alias·resource deployment 선택·fallbacks/weight/max_fallbacks 파싱·미지원 Python 전용 키 시작 시 경고 이식 |

## 완료 판정

1. 원본 공개 API와 OpenAI 호환 HTTP 계약을 Go contract test가 검증한다.
2. 의존 Python import와 Python runtime은 Go release image 및 binary runtime 경로에 남지 않는다.
3. 모든 원본 Python 파일은 manifest에서 `이식`, `삭제`, 또는 `비대상` 사유로 분류된다.

## 검증 기록

- `--local-dev`(embedded PostgreSQL + miniredis) 기동·인증·모델 조회 E2E 통과.
- Docker 컨테이너 배포 검증: distroless 이미지에 Python 런타임(`/python`, `/uvicorn`)이 없고, 외부 PostgreSQL·Redis에 연결해 `/health/readiness` 200·`/v1/models` 200 반환.
- Python 런타임 제거(2026-08-27): 원본 Python 파일 9,290여 개와 Python packaging·test·CI·빌드 자산 삭제 후 `go build`·`go vet`·`gofmt`·`go test ./...` green, distroless 이미지에 `.py`/`python*`/`uvicorn*`/`.so` 0개, `/health/liveliness` 200.
- SSE 스트리밍 응답의 usage/cost 기록 지원(최종 chunk의 `usage` 파싱).
- Python-Go contract 비교(실 Python proxy 대비, 동일 fixture):
  - 에러 envelope `{"error":{...}}`, 상태 코드(401/403/400), 인증→모델 접근(403 `key_model_access_denied`)→모델 존재 순서 일치.
  - `/v1/models`가 model group alias(예: `default`)를 노출.
  - `/key/delete`가 Python 계약의 `{"keys":[...]}` 입력과 `{"deleted_keys":[...]}` 응답을 사용(레거시 `{"key"}`도 호환).
  - management 라우트 HTTP method 정렬: `PATCH /organization/update`, `DELETE /organization/delete`(`organization_ids`), `DELETE /project/delete`(`project_ids`), 삭제 응답은 삭제된 객체 배열.
  - `/key/generate {}`의 nil models 500 버그 수정(빈 배열 정규화).
  - route-level 불일치 6→3 축소. 잔여 3건은 모두 합의된 차이에 함:
    - `user/block`·`user/unblock`: Go의 추가 슈퍼셋 라우트. Python은 `/user/update`의 `blocked` 필드로 처리하고 Go도 동일하게 지원.
    - `GET /key/info` 무파라미터: 400(파라미터 누락) vs 404.
    - `GET /user/info` 무파라미터: Python은 호출자 user(SSO `default_user_id`)를 반환하지만 SSO/SCIM은 제외 범위. Go는 master key 기반 admin이라 파라미터 필수.
  - management key CRUD(generate/info/list/delete/block/unblock/update) 상태 코드·payload 실 Python proxy와 일치 확인.
  - response cache는 `litellm_settings.cache: true`로 opt-in(기본 off, Python 기본값과 동일). cache hit에도 요청마다 usage/spend 기록 — 실 Python proxy(cache on) 3 요청 → 3행, Go 30 동시 요청 → 30행(총 300 tokens)으로 "usage 정확히 한 번 기록" 수용기준 충족 확인.
- 부하·장애(resilience) 검증 — 실 PostgreSQL·Redis + mock provider:
  - provider 장애: down 시 `502 upstream_error`, 복구 즉시 `200` 재처리 성공.
  - Redis 장애: chat 요청은 degraded로 `200`(rate limit·cache 우회), `/health/readiness`는 `503`. 복구 후 `200`.
  - PostgreSQL 재기동: 재기동 중 chat은 `200`(in-memory key·provider 호출 정상), 재기동 후 usage 기록 정상.
  - graceful shutdown: SIGTERM 수신 시 in-flight 요청 drain 후 exit code 0.
- 전환 지표(shadow/canary/rollback) 정의:
  - shadow: Python·Go 동일 트래픽 mirroring. 비교 지표 = 상태 코드, 응답 body(hash), usage/tokens, cost, latency p50/p95. 기준 = 24h 동안 99.9% 상태 코드 일치 + 100% usage/tokens 일치.
  - canary: 트래픽 1% → 10% → 50% → 100% 단계 증량. 단계 유지 1h 이상. 회귀 기준 = 5xx 비율 +10% 이상 또는 p95 latency +25% 이상 또는 usage 불일치 1건.
  - rollback: canary 단계별 즉시 Python proxy 복귀. trigger = 회귀 기준 충족, DB migration은 rollback 스크립트 유지(단방향 DDL 금지).
  - proxy 인증 실패의 `type`/`code`는 OpenAI 표준(`invalid_api_key`)을 유지. 이 repo의 Go SDK client contract test가 같은 값을 검증. Python의 `auth_error`/`token_not_found_in_db`는 상태 코드와 envelope은 동일하므로 합의된 차이로 기록.
  - upstream 401은 provider 응답을 그대로 전파(OpenAI SDK가 받는 형태와 동일).
