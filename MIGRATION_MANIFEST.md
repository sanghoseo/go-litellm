# OSS Python → Go Migration Manifest

## 기준

- 기준 커밋의 대상은 `litellm/` 2,229개와 `litellm-proxy-extras/` 7개 Python 파일이다.
- `enterprise/`, `ui/`, 배포된 wheel·tarball 및 Python test fixture는 이식 대상이 아니다.
- Go는 Python 파일명을 그대로 사용하지 않는다. Go가 `_`로 시작하는 파일을 무시하고, 단일 책임 패키지 구성이 필요하기 때문이다.
- 모든 행은 원본 Python contract, Go 구현, Go unit test, Python 대비 contract test가 모두 완료되어야 완료다.

| 단계 | 원본 영역 | Go 목적지 | 상태 |
| --- | --- | --- | --- |
| A | `litellm/_uuid.py`, `exceptions.py`, `_internal_context.py` | `litellm/` 공통 SDK | 부분 이식 |
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
- SSE 스트리밍 응답의 usage/cost 기록 지원(최종 chunk의 `usage` 파싱).
- Python-Go contract 비교(실 Python proxy 대비, 동일 fixture):
  - 에러 envelope `{"error":{...}}`, 상태 코드(401/403/400), 인증→모델 접근(403 `key_model_access_denied`)→모델 존재 순서 일치.
  - `/v1/models`가 model group alias(예: `default`)를 노출.
  - `/key/delete`가 Python 계약의 `{"keys":[...]}` 입력과 `{"deleted_keys":[...]}` 응답을 사용(레거시 `{"key"}`도 호환).
  - proxy 인증 실패의 `type`/`code`는 OpenAI 표준(`invalid_api_key`)을 유지. 이 repo의 Go SDK client contract test가 같은 값을 검증. Python의 `auth_error`/`token_not_found_in_db`는 상태 코드와 envelope은 동일하므로 합의된 차이로 기록.
  - upstream 401은 provider 응답을 그대로 전파(OpenAI SDK가 받는 형태와 동일).
