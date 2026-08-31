# AGENTS.md

## 빌드/검증

- `make build` — `bin/litellm-proxy` 빌드
- `make test` — Go 테스트 (변경 후에는 반드시 실행)
- `go vet ./...`와 `gofmt -l cmd internal pkg litellm`이 clean이어야 한다

## 컨벤션

- 새 엔드포인트는 TDD: contract 테스트 작성(red) 후 구현(green)
- Python proxy와 동일한 response shape 유지 (대시보드 계약). Python 원본은 이력에서 확인
- 코멘트는 필요한 곳에만 간결하게
- 커밋은 conventional commits (`feat:`/`fix:`/`docs:`/`chore:`)

## 문서

- [README.md](README.md) — 프로젝트 개요, 빌드/실행, 설정
- [API.md](API.md) — 대시보드 REST API 목록
- [UI.md](UI.md) — 대시보드 빌드/서빙
