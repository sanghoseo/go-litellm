# LiteLLM Go AI Gateway 전환 및 Enterprise 기능 PRD

## 1. 문서 목적

이 문서는 현재 LiteLLM의 Python 기반 AI Gateway와 진행 중인 Rust 구현을 Go 기반 서비스로 전환하기 위한 제품 범위, 우선순위, 호환성 기준 및 Enterprise 기능 요구사항을 정의한다. 구현 범위를 먼저 고정해 점진적 전환이 새로운 기능 개발과 기존 고객 워크로드의 안정성을 훼손하지 않도록 한다

## 2. 현황 및 근거

2026-08-20 저장소 조사 기준:

- Python 소스는 5,277개 파일이며, `litellm/`에는 3,401개의 추적 경로가 있다
- Python Proxy에는 약 757개의 HTTP 라우트 선언이 있으며, 관리, 인증, 비용, 관측성, OpenAI 호환 API 등 넓은 표면적을 가진다
- `litellm/llms/`에는 100개가 넘는 공급자 어댑터가 있다
- Rust 워크스페이스는 `core`, `ai-gateway`, `python-bridge` 크레이트로 분리되어 있고, messages, responses, audio transcription, OCR, realtime 및 일부 OpenAI, Anthropic, Bedrock, Azure AI, Vertex AI, Mistral 경로를 단계적으로 구현 중이다. 구성, 재시도, 라우팅 정책, 로깅, 비용 추적, 고객 플러그인은 여전히 Python이 담당한다
- Enterprise 코드는 상업 라이선스 경계 안에 있으며, 감사 로그, 키 및 프로젝트 관리, 내부 사용자 관리, SSO 확장, 관리형 파일·벡터 스토어, 알림 콜백 등의 기능이 존재한다
- 현 저장소의 Go 코드는 Terraform provider와 예제 수준으로, 실행 가능한 Go Gateway 모듈은 없다

결론적으로 본 프로젝트는 단순 언어 변환이 아니다. 데이터 계약, OpenAI 호환 API, 공급자별 변환, 인증·권한, 비용 정산 및 운영 기능을 보존하는 플랫폼 마이그레이션이다

## 3. 제품 비전

Go 기반 LiteLLM Gateway를 단일 고성능 배포 단위로 제공한다. 기존 LiteLLM Proxy 클라이언트와 운영 도구가 동일한 공개 API와 데이터 모델을 사용할 수 있어야 하며, Enterprise 고객은 중앙 인증, 세분화된 권한, 정책 집행, 비용 통제 및 감사를 기본 기능으로 사용해야 한다

## 4. 목표와 비목표

### 목표

1. OpenAI 호환 Gateway의 핵심 요청 경로를 Go로 운영 가능하게 만든다
2. 기존 Python Proxy와 병행 실행하면서 요청 단위로 안전하게 Go로 전환한다
3. 우선 공급자에 대해 요청·응답·스트리밍·오류 의미론의 호환성을 보장한다
4. Enterprise 핵심 제어면을 Go 서비스에 포함한다: 조직·프로젝트·팀·사용자·가상 키, RBAC, SSO, SCIM, 예산, 감사 로그 및 정책 기반 Guardrail 적용
5. 운영 데이터의 PostgreSQL 및 Redis 계약을 명시하고, 기존 배포·Terraform·관리 UI와 연동한다
6. 새 Enterprise 기능으로 정책-as-code 배포, 불변 감사 내보내기, 예산 임계치 자동 조치를 제공한다

### 비목표

- 첫 릴리스에서 Python SDK의 모든 함수 또는 100개 이상 공급자 전부를 Go SDK로 재작성하지 않는다
- Python 플러그인, 콜백, 임의 사용자 정의 Python 코드의 실행 환경을 Go Gateway 안에 포함하지 않는다
- 현재 Rust 코드의 기계적 포팅을 목표로 하지 않는다. 검증된 동작과 공개 계약을 기준으로 Go 설계를 새로 만든다
- Admin UI를 Go로 재작성하지 않는다. 초기에는 기존 UI가 사용하는 API 계약을 유지한다
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

### 6.1 Gateway 데이터면, P0

- OpenAI 호환 API: health/readiness, models, chat completions, responses, embeddings
- SSE 스트리밍, 요청 취소, context deadline 전파, 표준 오류 응답
- 공급자 어댑터 1차: OpenAI/Azure OpenAI, Anthropic, AWS Bedrock, Google Vertex AI/Gemini
- 인증: master key, 가상 키, JWT 기반 세션 토큰. 키의 조직·프로젝트·팀·사용자 범위 및 허용 모델 검증
- 라우팅: 모델 별칭, 가중치 분배, 우선순위, fallback, timeout, 재시도, circuit breaker
- 사용량 및 비용: 토큰 사용량 추출, 가격표 기반 비용 계산, 요청 로그, 일별·월별 집계
- Redis 기반 rate limit, 캐시 및 분산 조정. PostgreSQL 영속화
- 구조화 로그, Prometheus metrics, OpenTelemetry trace propagation 및 감사 이벤트 발행

### 6.2 Gateway 확장, P1

- Anthropic Messages 호환 API, audio transcription, image generation, rerank, batches, realtime/WebSocket
- 공급자 어댑터 2차: Mistral, Cohere, Groq, Ollama/vLLM 및 OpenAI-compatible endpoint
- 요청/응답 Guardrail 훅과 정책 상속
- semantic cache, prompt 관리, 파일·벡터 스토어 프록시, MCP/A2A는 각각 독립 RFC 승인 뒤 추가
- Python Proxy에서 아직 포팅되지 않은 경로는 compatibility proxy 모드로 위임한다. 이 위임은 관측 가능하고 설정으로 켜야 한다

### 6.3 Enterprise 제어면, P0

- 테넌시: organization, project, team, user, service account, virtual key의 명확한 소유 관계
- RBAC: system admin, organization admin, project admin, team admin, developer, auditor, viewer 역할과 최소 권한 API
- SSO: OIDC 우선 지원. SAML은 설계가 고정된 뒤 P1로 추가. JIT provisioning과 claim-to-role mapping 지원
- SCIM 2.0: Users/Groups CRUD, pagination, filter, PATCH, deprovisioning, idempotency 및 기존 team/user 모델 매핑
- 예산: 조직·프로젝트·팀·사용자·키 단위 한도, 모델 허용 목록, 기간, soft/hard threshold, 동시성 및 rate limit
- 감사: 인증, 권한 거부, 관리 변경, 키 수명주기, 정책 변경, 모델 라우팅 결정, 비용 한도 조치 기록
- 정책 엔진: 대상(조직·프로젝트·팀·키·모델), 상속, 우선순위, guardrail 및 라우팅 규칙 결합

### 6.4 신규 Enterprise 기능, P1

- 정책-as-code: Git 또는 API로 versioned policy bundle을 검증·승인·배포·롤백하고 적용 버전을 요청 및 감사 이벤트에 기록
- 불변 감사 내보내기: 고객 소유 object storage 또는 SIEM으로 순서 보장된 서명 이벤트 배치를 내보내고 체크포인트로 누락과 변조를 탐지
- 예산 자동 조치: soft threshold 알림, hard threshold 차단, fallback model 전환, 제한 완화 승인 흐름. 조치마다 정책·주체·근거를 감사 로그에 남김

## 7. 호환성 계약

Go Gateway는 다음 계약을 Python Proxy 기준으로 유지한다

- 공개 엔드포인트, HTTP method, 인증 header, OpenAI 형식의 request/response 및 SSE event 순서
- 상태 코드와 오류 형식. 호환 불가능한 개선은 versioned endpoint 또는 명시적 migration flag로 제공
- virtual key, user, team, project, budget 및 spend log의 식별자와 소유 관계
- 기존 YAML 구성의 P0 영역. Go 전용 설정은 별도 namespace 아래 두고, 지원하지 않는 필드는 시작 시 오류 또는 명확한 경고를 낸다
- Helm chart, Docker 환경변수, Terraform provider가 필요한 관리 API 계약

호환성 기준은 문서 비교가 아니라 contract test로 판정한다. 동일한 입력을 Python 기준 인스턴스와 Go 후보 인스턴스에 보내어 상태 코드, 필수 header, JSON 의미론, SSE 이벤트, 비용·사용량 기록, 감사 이벤트를 비교한다

## 8. 아키텍처 원칙

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

- 단일 Go module에서 시작하되, `cmd/gateway`와 `internal/` 아래 transport, auth, policy, routing, providers, usage, audit, admin 모듈을 분리한다
- provider adapter는 공통 인터페이스와 강타입 request/response 모델을 사용한다. HTTP handler가 공급자 변환이나 영속화를 직접 수행하지 않는다
- 인증, 권한, 예산, 정책 판정은 provider 호출 전에 완료된다. 비용 확정과 감사 기록은 요청 종료 시 실패 격리된 outbox를 통해 처리한다
- PostgreSQL은 source of truth, Redis는 TTL 상태, rate limit, cache 및 저지연 카운터 용도로만 사용한다
- 관리 변경과 사용량 업데이트는 idempotency key와 transactional outbox를 사용한다
- Enterprise 전용 코드는 명확한 라이선스 패키지 및 빌드 배포 경계에 둔다. OSS 바이너리는 Enterprise 권한 우회를 포함하지 않는다

## 9. 마이그레이션 단계 및 완료 기준

| 단계 | 제공 범위 | 완료 기준 |
| --- | --- | --- |
| 0. 기준선 | API inventory, 데이터 모델 매핑, golden fixtures, 성능·오류 기준 수집 | P0 endpoint와 Enterprise API의 owner, 요청 샘플, DB 영향, 지원 여부가 inventory에 기록 |
| 1. 기반 | Go module, config, HTTP/SSE, PostgreSQL, Redis, telemetry, auth skeleton | health와 가상 키 인증, trace 및 구조화 로그가 배포 환경에서 동작 |
| 2. P0 데이터면 | chat/responses/embeddings, 1차 provider, routing, usage/spend | contract suite 통과 및 shadow traffic 비교에서 허용 불일치 없음 |
| 3. Enterprise P0 | tenancy/RBAC, SSO, SCIM, budgets, audit, policy | 권한 상승, tenant 경계, deprovisioning, budget race에 대한 보안·통합 테스트 통과 |
| 4. 점진 전환 | shadow, canary, request-level rollback, compatibility proxy | 운영 대시보드에서 Python/Go 비교가 가능하고 tenant별 rollback이 즉시 가능 |
| 5. P1 확장 | 확장 endpoint, 2차 provider, 신규 Enterprise 기능 | 각 기능의 RFC, contract test, 부하·복구 검증 및 운영 runbook 승인 |
| 6. Python 축소 | 포팅된 Gateway path의 Python 의존성 제거 | 지원 종료 공지, migration guide, 데이터 검증과 rollback 기간 종료 후 제거 |

## 10. 기능 요구사항과 수용 기준

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

## 11. 비기능 요구사항

- 보안: TLS 종료 뒤에도 trusted proxy 경계를 검증하고, secret은 로그·감사 payload·오류에 기록하지 않는다. 키는 해시 또는 KMS envelope encryption으로 저장한다
- 신뢰성: provider, Redis, exporter 실패가 인증·정책·감사 데이터 정합성을 우회하지 않아야 한다. 명시된 fail-open/fail-closed 정책을 endpoint별로 둔다
- 성능: P0 기준선은 기존 Python Proxy와 같은 인프라와 공급자 조건에서 P95 Gateway overhead와 오류율을 비교해, 사전에 합의한 SLO를 충족해야 한다. 정확한 수치는 단계 0 측정 후 결정한다
- 관측성: request ID, tenant ID의 비식별 참조, model, route, provider, retry, latency, token/cost outcome을 correlation 가능하게 기록한다
- 운영성: config validation, readiness, graceful shutdown, migration 상태 확인, metric/trace/log export, tenant별 canary와 rollback을 제공한다

## 12. 데이터 및 API 전환 원칙

- 기존 Prisma/PostgreSQL 스키마를 조사해 변경 전에는 schema compatibility matrix와 migration plan을 만든다
- Go는 기존 테이블을 직접 쓰더라도 ORM 모델을 진실의 원천으로 만들지 않는다. 쿼리와 transaction 경계를 명시적으로 소유한다
- 사용량, spend log, audit event는 append-only 우선이며 집계 테이블은 재생성 가능해야 한다
- API 삭제·필드 의미 변경은 금지한다. 새 필드는 additive하게 도입하고 client capability를 확인할 수 없는 변경은 versioning한다

## 13. 테스트 및 출시 게이트

- API contract: Python 기준과 Go 후보 간 fixture 및 실제 provider sandbox/계정 기반 비교
- 통합: PostgreSQL, Redis, OIDC test provider, SCIM client, object storage/SIEM exporter, 1차 provider별 성공·오류·timeout·streaming 검증
- 보안: tenant isolation, RBAC, key rotation/revocation, forged JWT, SCIM replay, budget concurrency, policy bypass, audit tampering 검증
- 회귀: 기존 관리 UI와 Terraform provider의 P0 흐름을 E2E로 검증
- 성능·복구: sustained load, provider outage, Redis 재시작, DB failover, exporter backlog, graceful shutdown
- 출시: shadow -> 내부 canary -> 선택 tenant canary -> 기본 전환 순서로 진행하며, 오류율·지연·비용 차이·권한 거부율에 자동 rollback threshold를 설정

## 14. 리스크와 결정 필요 사항

| 리스크 | 대응 |
| --- | --- |
| 전체 Python 기능을 동등하게 포팅하려는 범위 팽창 | P0/P1 inventory와 contract test가 없는 endpoint는 전환 대상에 포함하지 않음 |
| Rust 작업과 Go 전환의 중복 투자 | Rust를 동작 및 fixture의 참고 구현으로 분류하고, 새 Gateway 기능의 단일 소유자를 Go로 지정 |
| Python callback/plugin 의존 | 지원 가능한 webhook/event contract를 정의하고, 불가한 플러그인은 compatibility proxy로 명시적 위임 |
| 비용/예산의 경쟁 조건 | DB 트랜잭션 또는 원자 카운터, idempotency, reconciliation job 및 보수적 hard-limit 정책 적용 |
| Enterprise 라이선스 혼합 | OSS/Enterprise package, 빌드 산출물, CI 및 배포 권한을 분리하고 법무·제품 승인을 거침 |
| 기존 DB 의미론 불명확 | 코드 포팅 전 schema 및 운영 데이터 샘플 기반의 compatibility matrix를 승인 |

착수 전 제품 책임자가 결정해야 할 사항은 다음과 같다: Go 전환의 최초 운영 대상이 self-hosted Gateway만인지 hosted control plane까지인지, P0에서 반드시 지원할 공급자와 endpoint, 기존 Enterprise 계약과 신규 기능의 배포·라이선스 정책, 허용 가능한 Python compatibility proxy 존속 기간, 그리고 단계 0에서 확정할 성능·가용성 SLO다

## 15. 산출물

- `go.mod`, Go Gateway 바이너리, container/Helm 배포물 및 운영 runbook
- API·설정·DB compatibility matrix와 자동 contract test suite
- P0 Enterprise control plane, migration 및 rollback 도구
- 정책-as-code, 감사 export, 예산 자동 조치에 대한 RFC와 구현·보안 테스트
- Python 경로별 포팅 상태표와 deprecation/migration guide
