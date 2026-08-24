# Rust 구현 범위와 Go 전환 설계

## 목적

이 문서는 `litellm-rust/`의 현재 구현 범위를 확인하고, 대시보드를 제외한 모든 코드를 Go로 재개발하는 최종 전환에서 각 Rust 기능을 어떤 Go 구성요소로 대체할지 정의한다. 이 문서는 Rust 코드를 기계적으로 번역하기 위한 문서가 아니다. 공개 계약과 운영 동작을 보존하면서 Python·Rust 의존성을 제거하기 위한 inventory다

Rust도 Python과 동일하게 먼저 파일 책임과 test assertion을 1:1 Go parity 구현으로 대응한다. Go package 통합·파일 분할·중복 제거는 parity gate 통과 후 별도의 refactor 단계에서만 수행한다

## 조사 기준

2026-08-24 기준 Rust workspace는 세 크레이트로 구성된다

| Rust 크레이트 | 런타임 규모 | 현재 책임 | Go 전환 결과 |
| --- | ---: | --- | --- |
| `litellm-core` | 57 파일, 약 5,811 LOC | provider 호출, 변환, route type, router, lifecycle, in-memory cache | `internal/providers`, `internal/routes`, `internal/routing`, `internal/lifecycle`, `internal/cache` |
| `litellm-ai-gateway` | 46 파일, 약 6,942 LOC | Axum HTTP/WS host, realtime, OCR/transcription legacy host 경로, auth, callback integration | `cmd/gateway`, `internal/httpapi`, `internal/realtime`, `internal/auth`, `internal/hooks`, `internal/observability` |
| `litellm-python-bridge` | 3 파일, 약 489 LOC | PyO3 변환, Python sync/async wrapper, GIL release | 제거. Go SDK 또는 Go Gateway HTTP API로 대체 |

Rust 테스트 표시는 약 33개 파일, 164개의 `test` attribute가 있다. 이 테스트와 Python parity/E2E 테스트의 assertion을 language-neutral fixture로 추출한 뒤 `go test`로 재작성한다. 대시보드 UI 테스트를 제외한 Python/Rust test source는 최종 저장소에서 제거한다

## 현재 의존성과 전환 원칙

```text
현재
Python Proxy / Python SDK
        | PyO3 bridge, callback HTTP API, YAML config reader
Rust core <- Rust ai-gateway <- Rust python-bridge
        |                         |
    provider HTTP/WS          Python objects / GIL

최종
Next.js Dashboard / Go SDK / OpenAI clients
        |
Go Gateway + Go Worker
        |
auth -> policy -> routing -> provider HTTP/WS -> usage/audit outbox
        |
PostgreSQL + Redis/Valkey
```

- Rust의 provider 변환, protocol 규칙, error mapping, request/response fixture는 Go 재구현 대상이다
- PyO3, GIL, embedded Python config reader, Python Proxy callback API는 보존 대상이 아니라 제거 대상이다
- 현재 Rust가 Python에 위임하는 config, rollout, fallback, spend tracking, callback은 Go Gateway/Worker의 native 기능으로 옮긴다
- Go 구현의 완료 기준은 Rust 구현의 존재가 아니라 Python/Rust 기준과의 contract test 통과, 기존 비-UI test/tooling의 Go 재작성, 그리고 production Go-only deployment다

## 기능별 inventory 및 Go 매핑

### 1. `litellm-core`

| Rust 경로 | 현재 동작 | Go 대상 | 전환 시 확인할 계약 |
| --- | --- | --- | --- |
| `messages/` | Anthropic Messages 요청 준비, 인증, provider HTTP, stream 및 non-stream 응답 | `internal/routes/messages`, `internal/providers/anthropic`, `internal/providers/azureai` | system prompt/content block, cache control, header, timeout, SSE, 오류 shape |
| `responses/` | OpenAI Responses 변환, WebSocket connection, usage instrumentation | `internal/routes/responses`, `internal/providers/openai`, `internal/realtime/responses` | WebSocket upgrade, 양방향 frame 순서, usage/cost completion, close/error semantics |
| `realtime/` | OpenAI realtime type 및 transform | `internal/realtime`, `internal/providers/openai` | WebSocket session, event transform, cancellation, reconnect/close |
| `ocr/` | OCR request type과 변환 | `internal/routes/ocr`, `internal/providers/azureai`, `internal/providers/mistral`, `internal/providers/vertexai` | document payload 처리, polling, response normalization, PII 비로그 정책 |
| `audio_transcription/` | audio transcription type과 auth transform | `internal/routes/audio`, `internal/providers/bedrock` | model/region 결정, AWS auth, multipart/JSON payload, timeout/error |
| `providers/bedrock/aws_base.rs` | AWS credentials와 SigV4 request signing | `internal/providers/bedrock/auth.go` | default chain, assume role, region precedence, signed request fixture |
| `router/` 및 `routing_utils/` | deployment model과 simple shuffle 선택 | `internal/routing` | model alias, candidate selection, no-candidate error. Python의 전체 fallback/strategy와 통합 필요 |
| `call_lifecycle/` | 단계 timing, usage 축적, callback payload 구성 | `internal/lifecycle`, `internal/usage` | phase order, failure event, timing, callback payload 및 cost 계산 |
| `caching/in_memory_cache.rs` | TTL/LRU 성격의 process-local cache | `internal/cache/memory.go` 및 Redis/Valkey adapter | TTL, eviction, key format, cache hit/miss metric. multi-instance는 Redis/Valkey가 기준 |
| `error.rs` 및 `constants.rs` | typed error, shared constants | `internal/apperror`, `internal/constants` | caller에 노출되는 status/error code, upstream body의 sanitization |

### 2. `litellm-ai-gateway`

현재 server feature에서 실제 등록된 HTTP/WS route는 다음과 같다

| Rust route | Go route 책임 | 상태 |
| --- | --- | --- |
| `GET /health/liveness` | liveness | 직접 포팅 |
| `GET /health/readiness` | readiness와 dependency 상태 | 직접 포팅, PostgreSQL/Redis dependency policy 명시 |
| `GET /health/gil` | Python GIL activity | 제거. Go runtime health/metrics로 대체 |
| `POST /v1/messages` | Anthropic Messages | 직접 포팅 |
| `GET /v1/realtime` | realtime WebSocket upgrade | 직접 포팅 |
| `GET /v1/responses`, `GET /responses` | Responses WebSocket upgrade | 직접 포팅 후 OpenAI API method/upgrade compatibility 검증 |

그 밖의 주요 구현은 아래와 같다

| Rust 경로 | 현재 동작 | Go 전환 방식 |
| --- | --- | --- |
| `io/realtime.rs`, `io/realtime_pool.rs` | upstream realtime WebSocket splice, warm pool, retry/backoff | `internal/realtime/pool.go`. Goroutine lifecycle, bounded queue, cancellation, per-upstream capacity, metrics를 명시적으로 관리 |
| `io/responses_ws.rs` | Responses 양방향 WebSocket forwarding | `internal/realtime/responses_ws.go`. inbound/outbound frame order와 close code contract test 필요 |
| `routes/messages`, `routes/responses`, `routes/realtime` | Axum transport, model validation, host-to-core dispatch | `internal/httpapi`. HTTP handler는 인증·정책·transport만 담당하고 provider logic은 route/provider 계층으로 분리 |
| `auth/mod.rs` | master key constant-time comparison, SHA-256 token hash | `internal/auth`. timing-safe compare, hash format, virtual key와 RBAC로 확장 |
| `integrations/custom_guardrail` | pre/during-call custom guardrail orchestration | `internal/hooks/guardrail`. Python class callback 대신 Go interface 또는 external webhook contract 제공 |
| `integrations/custom_logger` | success/failure callback dispatch와 bounded queue | `internal/hooks/logging`, `internal/usage/outbox`. delivery retry, backpressure, dead-letter metric을 설계 |
| `integrations/litellm_python_proxy_api` | 완료된 realtime session을 Python Proxy에 POST해 spend/callback 처리 | 제거. Go worker가 PostgreSQL outbox에서 spend, audit, callback을 직접 처리 |
| `ocr/`, `audio_transcription/` | Rust host에 남은 legacy provider call/lifecycle 구현 | Go route/provider 계층으로 옮김. Rust core가 목표로 하던 경계보다 Go 최종 경계를 우선 |
| `python/config.rs` 및 `python-config` feature | embedded Python으로 Proxy YAML을 읽어 router 생성 | 제거. Go native YAML parser와 schema validation으로 구현 |
| `gil.rs`, `routes/gil.rs` | GIL release counter | 제거. Go에는 GIL이 없으며 goroutine, queue depth, GC, active stream metric을 제공 |

### 3. `litellm-python-bridge`

Python bridge는 `_native` cdylib를 생성하고 다음 Python 호출을 노출한다

- OCR: `ocr`, `aocr`
- audio transcription: `transcription`, `atranscription`
- Anthropic Messages: `messages`, `amessages`
- Responses WebSocket connection class
- GIL 통계: `gil_stats`

Go 최종 전환에서는 이 bridge를 포팅하지 않는다. 다음처럼 대체한다

| 현재 bridge 역할 | Go 전환 결과 |
| --- | --- |
| Python object와 JSON 변환 | Go HTTP handler 또는 Go SDK의 typed struct와 JSON codec |
| Python sync/async wrapper | Go SDK의 context 기반 동기 호출과 channel/stream reader |
| GIL release | 불필요. 요청 context와 goroutine cancellation으로 대체 |
| Python bridge unavailable fallback | 단계적 배포에서는 Go endpoint canary/rollback으로 대체. 최종에는 제거 |

Python 경로 `litellm/rust_bridge/` 및 `litellm/ocr/main.py`, `litellm/llms/custom_httpx/llm_http_handler.py`에 Rust bridge 선택, feature flag, fallback 코드가 존재한다. 해당 Python 경로와 그 테스트는 Go 구현 및 Go test로 대체한 뒤 저장소에서 제거해야 한다

## Go에서 재설계해야 하는 항목

### 설정과 배포

Rust ai-gateway의 `python-config` feature는 Python interpreter와 `litellm` import를 요구한다. Go Gateway는 이를 사용하면 안 된다. 기존 YAML의 지원 범위를 schema로 정의하고, Go parser가 config validation, secret reference resolution, model deployment 생성, reload 및 rollout 상태를 소유해야 한다

### callback, spend, audit

Rust realtime Gateway는 완료된 session을 Python Proxy callback API로 보내며, spend tracking과 기존 callback을 Python에 위임한다. Go에서는 요청 transaction과 durable outbox를 만들고 worker가 PostgreSQL 기준으로 usage, spend, audit 및 webhook을 처리해야 한다. external webhook 실패는 요청 성공/실패를 바꾸지 않되, Enterprise 정책이 필요하면 명시적 fail-closed로 설정한다

### WebSocket과 streaming

Rust의 `tokio-tungstenite` 구현은 Go의 `nhooyr.io/websocket` 또는 동등한 유지보수 가능한 라이브러리로 대체할 수 있다. 라이브러리 선택보다 더 중요한 계약은 frame forwarding, close code, context cancellation, upstream timeout, ping/pong, backpressure, warm pool에서의 connection ownership이다. 이를 fixture와 통합 테스트로 고정한다

### AWS 인증

Bedrock 경로는 AWS SDK for Go v2의 default credential chain과 SigV4 signer로 구현한다. Rust의 credential precedence와 role/region behavior를 tests fixture로 비교하고, 임의 서명 구현은 하지 않는다

## 포팅 순서

1. route와 Python bridge 진입점, Rust feature flag, provider 변환, 테스트 assertion을 포함한 상세 inventory를 자동화하고 language-neutral fixture를 만든다
2. Go shared foundation을 만든다: config, typed error, HTTP client, auth, request context, telemetry, PostgreSQL outbox, Redis/Valkey abstraction
3. `messages`와 Anthropic/Azure AI provider path를 Go로 구현하고 Python/Rust/Go contract test를 통과시킨다
4. `responses`와 realtime WebSocket을 구현한다. normal close, upstream error, cancellation, warm-pool concurrency를 우선 검증한다
5. OCR, audio transcription, Bedrock SigV4, Azure AI, Mistral, Vertex AI를 구현한다
6. Go native hooks, guardrails, logging, spend, audit를 구현하고 Python Proxy callback API 의존을 제거한다
7. Go native YAML config와 full router/fallback strategy를 구현하고 embedded Python config reader를 제거한다
8. Python bridge와 Rust binary/container, feature flag, compatibility callback, Python/Rust test·tooling source를 제거한다

## 완료 게이트

- `litellm-rust/`의 모든 runtime·test·tooling 파일은 Go package/test 또는 명시적 제거 사유와 연결되어야 한다
- `port` Rust 파일은 원본 source 1개, Go parity source 1개, 대응 Go test와 migration ID를 가져야 한다. parity 단계에서 원본 책임을 합치거나 분할하지 않는다
- 각 Go route/provider는 Python과 Rust fixture에 대해 request serialization, response normalization, status/error, header, stream/WebSocket frame, usage/cost event를 비교해야 한다
- Go Gateway는 YAML을 직접 파싱하며 Python interpreter, PyO3, Rust binary를 포함하지 않아야 한다. 대시보드 외 CI/test/tool image도 Go toolchain만 사용해야 한다
- callback/spend/audit이 Go PostgreSQL outbox와 worker에서 동작하고 Python Proxy callback API 호출이 없어야 한다
- existing dashboard 및 Terraform provider E2E가 Go backend에서 통과해야 한다
- production Go-only canary가 Rust/bridge fallback 없이 운영 SLO를 충족한 뒤 Rust runtime artifact를 제거한다
