# go-litellm

LiteLLM OSS의 Python 구현을 Go로 재구현하는 프로젝트다. 최종 결과물은 단일 Go 바이너리 `litellm-proxy`로 배포하는 AI Gateway(Proxy)이며, OpenAI 호환 HTTP API와 관리 대시보드를 제공한다.

개발·빌드·배포에서 Python 런타임 의존성을 제거하는 것이 목적이다. 전환 범위는 [PRD.md](PRD.md)와 [MIGRATION_MANIFEST.md](MIGRATION_MANIFEST.md)에 정의되어 있다.

## 기능

- **OpenAI 호환 API** — `/v1/chat/completions`, `/v1/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/moderations`, `/v1/rerank`, `/v1/images/*`, `/v1/audio/*`, `/v1/batches` 등
- **라우팅** — `model_list` 기반 모델 그룹, alias, timeout/retry 설정
- **키 및 리소스 관리** — 가상 키, user, team, organization, project, budget의 CRUD 및 block/unblock
- **사용량 추적** — request별 spend log 저장(PostgreSQL)과 조회(`/spend/logs`, `/spend/logs/ui`)
- **관리 대시보드** — `ui/litellm-dashboard` (Next.js static export)를 `/ui` 프리픽스로 서빙해 제공. 로그인, Usage, Logs, Virtual Keys 등

## 요구 사항

- Go 1.27+
- (선택) PostgreSQL, Redis — 키/유저/팀/사용량 영속화와 레이트 리밋·응답 캐시에 사용. `--local-dev` 모드에서는 embedded PostgreSQL과 miniredis를 자동으로 실행해 별도 설치 없이 개발할 수 있다

## 빌드 및 실행

```bash
make build     # bin/litellm-proxy 생성
make run       # config.yaml로 proxy 실행
make local-dev # embedded PostgreSQL + miniredis와 함께 실행
make test      # Go 테스트
make test-race # race detector 포함 테스트
```

CLI 옵션:

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-config` | `config.yaml` | LiteLLM 호환 YAML 설정 경로 |
| `-env-file` | `.env` | 환경 변수 파일 경로 (선택) |
| `-listen` | `:4000` | HTTP 리스닝 주소 |
| `-local-dev` | `false` | local PostgreSQL/Redis 의존성 자동 기동 |

환경 변수:

| 변수 | 설명 |
|---|---|
| `LITELLM_MASTER_KEY` | 마스터 키 (`general_settings.master_key`가 `os.environ/...`일 때 사용) |
| `DATABASE_URL` | PostgreSQL DSN. 미설정 시 키/유저/팀/사용량 영속화 비활성 (마스터 키만 동작) |
| `REDIS_URL` | Redis 주소. 미설정 시 레이트 리밋/응답 캐시 비활성 |
| `UI_USERNAME` | 대시보드 로그인 ID (기본 `admin`) |
| `UI_PASSWORD` | 대시보드 로그인 비밀번호 (기본: 마스터 키) |
| `UI_LOGO_PATH` | navbar 로고 override (로컬 파일 또는 http(s) URL) |

## 설정 예시

```yaml
model_list:
  - model_name: gpt-4o-mini
    litellm_params:
      model: openai/gpt-4o-mini
      api_key: os.environ/OPENAI_API_KEY

general_settings:
  master_key: os.environ/LITELLM_MASTER_KEY
  resource_model: gpt-4o-mini

litellm_settings:
  request_timeout: 120
  num_retries: 2

router_settings:
  model_group_alias:
    default: gpt-4o-mini
```

## 사용 예시

```bash
curl -X POST http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## 관리 대시보드

`ui/litellm-dashboard`에서 static export로 빌드한 뒤 `/ui` 프리픽스로 서빙한다 (프로덕션은 nginx, `ui/nginx.conf` 참조). Go proxy에는 CORS 헤더가 없어 UI를 별도 origin으로 서빙하면 요청이 차단되므로 same-origin 구조가 필요하다.

```bash
cd ui/litellm-dashboard
npm ci
npm run build   # out/ 디렉토리에 static export 생성
```

로그인: `UI_USERNAME` (기본 `admin`) / `UI_PASSWORD` (기본: 마스터 키).

## 프로젝트 구조

```
cmd/litellm-proxy/   # 바이너리 엔트리포인트 (flag 파싱, local-dev 기동)
internal/
  httpapi/           # HTTP 라우팅 및 핸들러 (OpenAI API, 관리 API, 대시보드 API)
  config/            # LiteLLM 호환 YAML 설정 파싱
  auth/              # 마스터 키, 가상 키, user/team/org 저장소
  routing/           # 모델 그룹 라우팅
  providers/         # LLM provider 클라이언트
  store/             # PostgreSQL/Redis 저장소
  usage/             # spend log 기록/조회
  localdev/          # embedded PostgreSQL + miniredis
  observability/     # 메트릭
pkg/types/           # OpenAI 호환 요청/응답 타입
pkg/litellm/         # HTTP 클라이언트
litellm/             # 공용 유틸
ui/litellm-dashboard/ # 관리 대시보드 (Next.js, 기존 유지)
```

## 문서

- [PRD.md](PRD.md) — Go 전환 목적 및 방향
- [MIGRATION_MANIFEST.md](MIGRATION_MANIFEST.md) — Python → Go 전환 현황 (계약 기준)
- [PYTHON_TO_GO_SCOPE.md](PYTHON_TO_GO_SCOPE.md) — 재개발 범위
- [API.md](API.md) — 대시보드가 호출하는 REST API 전체 목록
- [ARCHITECTURE.md](ARCHITECTURE.md) — 리포지토리 아키텍처
- [UI.md](UI.md) — 대시보드 빌드/서빙 구조
