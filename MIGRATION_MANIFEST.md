# OSS Python → Go Migration Manifest

## 기준

- 기준 커밋의 대상은 `litellm/` 2,229개와 `litellm-proxy-extras/` 7개 Python 파일이다.
- `enterprise/`, `ui/`, 배포된 wheel·tarball 및 Python test fixture는 이식 대상이 아니다.
- Go는 Python 파일명을 그대로 사용하지 않는다. Go가 `_`로 시작하는 파일을 무시하고, 단일 책임 패키지 구성이 필요하기 때문이다.
- 모든 행은 원본 Python contract, Go 구현, Go unit test, Python 대비 contract test가 모두 완료되어야 완료다.

| 단계 | 원본 영역 | Go 목적지 | 상태 |
| --- | --- | --- | --- |
| A | `litellm/_uuid.py`, `exceptions.py`, `_internal_context.py` | `litellm/` 공통 SDK | 부분 이식 |
| B | `litellm/types/`, `types/` | `pkg/types/` | 대기 |
| C | `litellm/main.py`, `utils.py`, `router.py`, router strategy | `pkg/litellm/`, `internal/routing/` | 대기 |
| D | `litellm/llms/**`, provider transformations | `internal/providers/` | 대기 |
| E | `litellm/proxy/**` | `internal/httpapi/`, `internal/auth/`, `internal/store/` | 진행 중 |
| F | `litellm/integrations/**`, logging, callbacks | `internal/observability/`, `internal/integrations/` | 대기 |
| G | files, batches, audio, images, embeddings, vector stores | `internal/api/`, `internal/providers/` | 대기 |
| H | `litellm-proxy-extras/**`, `schema.prisma` | `internal/store/postgres/`, SQL migrations | 진행 중 |
| I | `docker/**`, `pyproject.toml`, `uv.lock`, `.env`, config | Dockerfile, `go.mod`, config loader, SQL migration | 진행 중 |

## 완료 판정

1. 원본 공개 API와 OpenAI 호환 HTTP 계약을 Go contract test가 검증한다.
2. 의존 Python import와 Python runtime은 Go release image 및 binary runtime 경로에 남지 않는다.
3. 모든 원본 Python 파일은 manifest에서 `이식`, `삭제`, 또는 `비대상` 사유로 분류된다.
