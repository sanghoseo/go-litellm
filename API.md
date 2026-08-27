# LiteLLM 프론트 REST API 목록

`ui/litellm-dashboard` 프론트에서 호출하는 REST API 전체 목록.
경로는 `proxyBaseUrl`(없으면 `/` 상대) 기준. 인증은 `x-litellm-api-key`(또는 커스텀 `litellm_header_name`) 헤더에 `Bearer ${accessToken}`로 전달.

## Auth / SSO

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| POST | `/v2/login` | 사용자 로그인 | `components/networking.tsx` `loginCall` |
| POST | `/v3/login` | 로그인 (v3, code flow) | `components/networking.tsx` |
| POST | `/v3/login/exchange` | code → JWT 교환 | `components/networking.tsx` `exchangeLoginCode` |
| GET | `/get/sso_settings` | SSO 설정 조회 | `components/networking.tsx` |
| PATCH | `/update/sso_settings` | SSO 설정 수정 | `components/networking.tsx` |
| GET | `/sso/get/ui_settings` | SSO UI 설정 조회 | `components/networking.tsx` |
| GET | `/sso/key/generate` | SSO 키 생성 (브라우저 리다이렉트) | `components/user_dashboard.tsx` |

## User

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/user/list` | 사용자 목록 | `components/networking.tsx` `userListCall` |
| POST | `/user/new` | 사용자 생성 | `components/networking.tsx` `userCreateCall` |
| POST | `/user/update` | 사용자 수정 | `components/networking.tsx` `userUpdateCall` |
| POST | `/user/delete` | 사용자 삭제 | `components/networking.tsx` `userDeleteCall` |
| GET | `/user/info` | 사용자 정보 조회 | `components/networking.tsx` `userGetInfoCall` |
| GET | `/v2/user/info` | 사용자 정보 조회 (v2) | `components/networking.tsx` `userGetInfoV2` |
| POST | `/user/bulk_update` | 사용자 일괄 수정 | `components/networking.tsx` |
| GET | `/user/available_roles` | 사용 가능한 역할 목록 | `components/networking.tsx` |
| GET | `/user/available_users` | 사용 가능한 사용자 목록 | `components/networking.tsx` `userGetAllAvailableUsers` |
| GET | `/user/filter/ui` | 사용자 필터 UI 데이터 | `components/networking.tsx` |
| GET | `/user/daily/activity` | 사용자 일별 활동 (분 페이지) | `components/networking.tsx` `userDailyActivityCall` |
| GET | `/user/daily/activity/aggregated` | 사용자 일별 활동 요약 | `components/networking.tsx` |
| GET | `/get/internal_user_settings` | 내부 사용자 설정 조회 (기본값) | `DefaultUserSettingsForm.tsx` (`fetchClient`) |
| PATCH | `/update/internal_user_settings` | 내부 사용자 설정 수정 | `DefaultUserSettingsForm.tsx` (`fetchClient`) |

## Key

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| POST | `/key/generate` | 키 생성 | `components/networking.tsx` `keyCreateCall` |
| POST | `/key/update` | 키 수정 | `components/networking.tsx` `keyUpdateCall` |
| POST | `/key/delete` | 키 삭제 | `components/networking.tsx` `keyDeleteCall` |
| GET | `/key/list` | 키 목록 | `components/networking.tsx` `keyListCall` |
| GET | `/key/info` | 키 정보 조회 | `components/networking.tsx` `keyInfoCall`(구) |
| POST | `/v2/key/info` | 키 정보 일괄 조회 (v2) | `components/networking.tsx` `keyInfoCall` |
| GET | `/key/aliases` | 키 alias 목록 | `components/networking.tsx` `keyInfoAliases` |
| POST | `/key/regenerate` | 키 재생성 (alias 기반) | `components/networking.tsx` |
| POST | `/key/{key_id}/regenerate` | 키 재생성 (key_id 기반) | `components/networking.tsx` `regenerateKeyCall` |
| POST | `/key/block` | 키 차단 | `useSetKeyBlockedState.ts` |
| POST | `/key/unblock` | 키 차단 해제 | `useSetKeyBlockedState.ts` |
| POST | `/key/bulk_update` | 키 일괄 수정 | `components/TeamSSOSettings.tsx` (권한 경로) |
| POST | `/key/{key_id}/reset_spend` | 키 스펄 초기화 | `useResetKeySpend.ts` |
| POST | `/key/service-account/generate` | 서비스 계정 키 생성 | `components/networking.tsx` |

## Team

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/team/list` | 팀 목록 (v1) | `components/networking.tsx` `teamListCall` |
| GET | `/v2/team/list` | 팀 목록 (v2) | `components/networking.tsx` `v2TeamListCall` |
| POST | `/team/new` | 팀 생성 | `components/networking.tsx` `teamCreateCall` |
| POST | `/team/update` | 팀 수정 | `components/networking.tsx` `teamUpdateCall` |
| POST | `/team/delete` | 팀 삭제 | `components/networking.tsx` `teamDeleteCall` |
| GET | `/team/info` | 팀 정보 조회 | `components/networking.tsx` `teamInfoCall` |
| GET | `/team/available` | 사용 가능 팀 목록 | `components/networking.tsx` `teamGetCall` |
| POST | `/team/member_add` | 팀 멤버 추가 | `components/networking.tsx` `teamAddMemberCall` |
| POST | `/team/member_delete` | 팀 멤버 제거 | `components/networking.tsx` `teamRemoveMemberCall` |
| POST | `/team/member_update` | 팀 멤버 수정 | `components/networking.tsx` |
| POST | `/team/bulk_member_add` | 팀 멤버 일괄 추가 | `components/networking.tsx` |
| GET | `/team/{teamId}/members/me` | 내 팀 멤버 정보 | `components/team/useMyTeamMember.ts` |
| GET | `/team/daily/activity` | 팀 일별 활동 (분 페이지) | `components/networking.tsx` `teamDailyActivityCall` |
| GET | `/team/daily/activity/aggregated` | 팀 일별 활동 요약 | `components/networking.tsx` |
| GET | `/team/metadata_schema` | 팀 메타데이터 스키마 | `useTeamMetadataSchema.ts` |
| GET | `/team/permissions_list` | 팀 권한 목록 | `components/networking.tsx` |
| POST | `/team/permissions_update` | 팀 권한 수정 | `components/networking.tsx` |

## Organization

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/organization/list` | 조직 목록 | `components/networking.tsx` `orgListCall` |
| POST | `/organization/new` | 조직 생성 | `OrgCreateDialog.tsx` (`fetchClient`) |
| PATCH | `/v2/organization/{organization_id}` | 조직 수정 | `OrgSettingsForm.tsx` (`fetchClient`) |
| PATCH | `/organization/update` | 조직 수정 (v1) | `components/networking.tsx` `orgUpdateCall` |
| DELETE | `/organization/delete` | 조직 삭제 | `components/networking.tsx` `orgDeleteCall` |
| GET | `/organization/info` | 조직 정보 조회 | `components/networking.tsx` `orgInfoCall` |
| POST | `/organization/member_add` | 조직 멤버 추가 | `components/networking.tsx` `orgAddMemberCall` |
| PATCH | `/organization/member_update` | 조직 멤버 수정 | `components/networking.tsx` `orgUpdateMemberCall` |
| DELETE | `/organization/member_delete` | 조직 멤버 제거 | `components/networking.tsx` `orgRemoveMemberCall` |
| GET | `/organization/daily/activity` | 조직 일별 활동 (분 페이지) | `components/networking.tsx` `orgDailyActivityCall` |

## Model / Model Group

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/models` | 사용 가능한 모델 목록 | `components/networking.tsx` `modelAvailableCall` |
| GET | `/v1/model/info` | 모델 정보 (litellm_model_id 기준) | `components/networking.tsx` `getModelInfoWithID` |
| GET | `/v2/model/info` | 모델 정보 (v2) | `components/networking.tsx` `v2_model_infoCall` |
| POST | `/model/new` | 모델 추가 | `components/networking.tsx` `modelCreateCall` |
| PATCH | `/model/{model_id}/update` | 모델 수정 | `components/networking.tsx` `modelUpdateCall` |
| POST | `/model/delete` | 모델 삭제 | `components/networking.tsx` `modelDeleteCall` |
| GET | `/model_group/info` | 모델 그룹 목록 | `components/networking.tsx` `modelGroupListCall` |
| POST | `/model_group/make_public` | 모델 그룹 공개 전환 | `components/networking.tsx` `makeModelGroupPublic` |
| GET | `/model/cost_map/source` | 모델 비용 표 출처 | `components/networking.tsx` |
| POST | `/model_hub/update_useful_links` | 모델 허브 유용 링크 수정 | `components/networking.tsx` |

## Cost Map / Cost Optimization

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| POST | `/reload/model_cost_map` | 모델 비용 표 재로드 요청 | `components/networking.tsx` |
| POST | `/schedule/model_cost_map_reload?hours={hours}` | 비용표 자동 재로드 예약 | `components/networking.tsx` `scheduleModelCostMapReload` |
| DELETE | `/schedule/model_cost_map_reload` | 예약 취소 | `components/networking.tsx` `cancelModelCostMapReload` |
| GET | `/schedule/model_cost_map_reload/status` | 예약 상태 조회 | `components/networking.tsx` |
| GET | `/auto_router/shadow_eval` | 섀adow eval 잡 목록 | `useShadowEval.ts` |
| GET | `/auto_router/shadow_eval/{job_id}` | 섀adow eval 잡 상세 | `useShadowEval.ts` |
| POST | `/auto_router/shadow_eval/start` | 섀adow eval 시작 | `useShadowEval.ts` (`fetchClient`) |
| POST | `/auto_router/shadow_eval/{job_id}/stop` | 섀adow eval 중지 | `useShadowEval.ts` (`fetchClient`) |
| GET | `/auto_router/benchmarks` | 벤치마크 목록 | `useAutoRouterBenchmarks.ts` |
| GET | `/auto_router/test_routing` | 라우팅 테스트 상세 | `components/networking.tsx` `getAutoRouterRoutingTest` (raw fetch, GET) |
| POST | `/auto_router/test_routing` | 라우팅 테스트 실행 | `components/networking.tsx` `runAutoRouterRoutingTest` |
| GET | `/auto_router/classifier/default_prompt` | classifier 기본 프롬프트 | `components/networking.tsx` `getClassifierDefaultPrompt` |

## Credential (BYOK)

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/credentials` | 자격 증명 목록 | `components/networking.tsx` `credentialListCall` |
| GET | `/credentials/by_name/{credentialName}` | 이름 기준 조회 | `components/networking.tsx` `credentialGetCall` |
| GET | `/credentials/by_model/{modelId}` | 모델 기준 조회 | `components/networking.tsx` `credentialGetCall` |
| POST | `/credentials` | 자격 증명 생성 | `components/networking.tsx` `credentialCreateCall` |
| PATCH | `/credentials/{credentialName}` | 자격 증명 수정 | `components/networking.tsx` `credentialUpdateCall` |
| DELETE | `/credentials/{credentialName}` | 자격 증명 삭제 | `components/networking.tsx` `credentialDeleteCall` |

## Guardrail / Policy

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/guardrails/list` | 가드레일 목록 | `components/networking.tsx` `guardrailListCall` |
| GET | `/v2/guardrails/list` | 가드레일 목록 (v2) | `components/networking.tsx` |
| POST | `/guardrails` | 가드레일 생성(등록) | `components/networking.tsx` |
| PATCH | `/guardrails/{guardrail_id}` | 가드레일 수정 | `components/networking.tsx` `updateGuardrailCall` |
| DELETE | `/guardrails/{guardrail_id}` | 가드레일 삭제 | `components/networking.tsx` |
| GET | `/guardrails/{guardrail_id}/info` | 가드레일 정보 | `components/networking.tsx` `guardrailInfoCall` |
| POST | `/guardrails/test_custom_code` | 커스텀 코드가드레일 테스트 | `components/networking.tsx` `testCustomCodeGuardrailCall` |
| POST | `/guardrails/apply_guardrail` | 가드레일 적용(권한 위임) | `components/networking.tsx` |
| POST | `/guardrails/validate_blocked_words_file` | 차단 단어 파일 검증 | `components/networking.tsx` |
| GET | `/guardrails/submissions` | 팀 가드레일 제출 목록 | `components/networking.tsx` `getGuardrailSubmissions` |
| POST | `/guardrails/submissions/{guardrailId}/approve` | 제출 Approve | `components/networking.tsx` `approveGuardrailSubmission` |
| POST | `/guardrails/submissions/{guardrailId}/reject` | 제출 Reject | `components/networking.tsx` `rejectGuardrailSubmission` |
| GET | `/guardrails/ui/add_guardrail_settings` | 가드레일 추가 UI 설정 | `components/networking.tsx` |
| GET | `/guardrails/ui/major_airlines` | 항공사 목록 (예시 데이터) | `components/networking.tsx` |
| GET | `/guardrails/ui/category_yaml/{type}` | 카테고리 yaml 조회 | `components/networking.tsx` |
| GET | `/guardrails/ui/provider_specific_params` | 프로바이더 특이 파라미터 | `components/networking.tsx` |
| GET | `/guardrails/usage/overview` | 가드레일 사용량 요약 | `components/networking.tsx` `getGuardrailsUsageOverview` |
| GET | `/guardrails/usage/logs` | 가드레일 사용 로그 | `components/networking.tsx` `getGuardrailsUsageLogs` |
| GET | `/guardrails/usage/detail/{usage_id}` | 가드레일 사용 상세 | `components/networking.tsx` |
| GET | `/policies/list` | 정책 목록 | `components/networking.tsx` |
| GET | `/policies/{policyId}` | 정책 상세 | `components/networking.tsx` |
| POST | `/policies` | 정책 생성 | `components/networking.tsx` |
| PUT | `/policies/{policyId}` | 정책 수정 | `components/networking.tsx` |
| DELETE | `/policies/{policyId}` | 정책 삭제 | `components/networking.tsx` |
| PUT | `/policies/{policyId}/status` | 정책 상태 전환 (활성/비활성) | `components/networking.tsx` |
| GET | `/policies/{policyId}/resolved-guardrails` | 정책별 적용 가드레일 목록 | `components/networking.tsx` |
| POST | `/policies/resolve` | 정책 resolve (해당 모델/팀) | `components/networking.tsx` |
| POST | `/policies/test-pipeline` | 정책 파이프라인 테스트 | `components/networking.tsx` |
| GET | `/policies/attachments/list` | 정책 첨부 목록 | `components/networking.tsx` |
| POST | `/policies/attachments` | 정책 첨부 생성 | `components/networking.tsx` |
| POST | `/policies/attachments/estimate-impact` | 첨부 영향도 추정 | `components/networking.tsx` |
| DELETE | `/policies/attachments/{attachmentId}` | 정책 첨부 삭제 | `components/networking.tsx` |
| GET | `/policies/name/{policyName}/versions` | 정책 버전 목록 | `components/networking.tsx` `listPolicyVersions` |
| POST | `/policies/name/{policyName}/versions` | 정책 버전 생성 | `components/networking.tsx` `createPolicyVersion` |
| GET | `/policy/info/{policyName}` | 정책 정보 (이름 기반) | `components/networking.tsx` |
| GET | `/policy/templates` | 정책 템플릿 목록 | `components/networking.tsx` |
| POST | `/policy/templates/suggest` | 템플릿 제안 | `components/networking.tsx` |
| POST | `/policy/templates/test` | 템플릿 테스트 | `components/networking.tsx` |
| POST | `/policy/templates/enrich` | 템플릿 enrich | `components/networking.tsx` |
| POST | `/policy/templates/enrich/stream` | 템플릿 enrich (SSE) | `components/networking.tsx` |
| POST | `/utils/test_policies_and_guardrails` | 정책+가드레일 통합 테스트 | `components/networking.tsx` |
| POST | `/utils/transform_request` | 변환된 요청 검증 | `components/networking.tsx` `transformRequestCall` |

## Budget

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/budget/list` | 예산 목록 | `components/networking.tsx` `budgetListCall` |
| POST | `/budget/new` | 예산 생성 | `components/networking.tsx` `budgetCreateCall` |
| POST | `/budget/update` | 예산 수정 | `components/networking.tsx` `budgetUpdateCall` |
| POST | `/budget/delete` | 예산 삭제 | `components/networking.tsx` `budgetDeleteCall` |
| GET | `/management/v1/budgets` | 예산 목록 (management v1) | `useBudgets.ts` |

## Tag

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/tag/list` | 태그 목록 | `components/networking.tsx` `tagListCall` |
| POST | `/tag/new` | 태그 생성 | `components/networking.tsx` `tagCreateCall` |
| POST | `/tag/update` | 태그 수정 | `components/networking.tsx` `tagUpdateCall` |
| POST | `/tag/delete` | 태그 삭제 | `components/networking.tsx` `tagDeleteCall` |
| POST | `/tag/info` | 태그 정보 저장 | `components/networking.tsx` `tagInfoCall` |
| GET | `/tag/daily/activity` | 태그 일별 활동 (분 페이지) | `components/networking.tsx` `tagDailyActivityCall` |
| GET | `/tag/summary` | 태그 요약 | `components/networking.tsx` `tagSummaryCall` |
| GET | `/tag/dau` / `/tag/wau` / `/tag/mau` | 일/주/월 활성 | `components/networking.tsx` |
| GET | `/tag/distinct` | 구분 개체 수 | `components/networking.tsx` |
| GET | `/tag/user-agent/per-user-analytics` | user-agent별 분석 | `components/networking.tsx` |

## Customer

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/customer/list` | 고객 목록 | `components/networking.tsx` `customerListCall` / `useCustomers.ts` |
| GET | `/customer/daily/activity` | 고객 일별 활동 (분 페이지) | `components/networking.tsx` `customerDailyActivityCall` |

## Spend / Usage / Activity

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/global/activity` | 글로벌 활동 (에러/토큰/지연) | `components/networking.tsx` `globalActivityCall` |
| GET | `/global/activity/model` | 모델별 활동 | `components/networking.tsx` `globalActivityModelCall` |
| GET | `/global/activity/cache_hits` | 캐시ヒット 활동 | `useCacheActivity.ts` (`$api.useQuery`) |
| GET | `/gateway/daily/activity` | 게이트웨이 일별 활동 | `components/networking.tsx` `gatewayDailyActivityCall` |
| GET | `/spend/logs/ui` | 스펄 로그 (UI) | `components/networking.tsx` `spendLogsCall` |
| GET | `/spend/logs/session/ui` | 세션별 스펄 로그 (UI) | `components/networking.tsx` |
| GET | `/global/spend/end_users` | 전체 스펄 end-user (POST) | `components/networking.tsx` |
| GET | `/global/spend/keys` | 키별 전체 스펄 | `components/networking.tsx` |
| GET | `/global/spend/models` | 모델별 전체 스펄 | `components/networking.tsx` |
| GET | `/global/spend/provider` | 프로바이더별 전체 스펄 | `components/networking.tsx` |
| GET | `/global/spend/teams` | 팀별 전체 스펄 | `components/networking.tsx` |
| GET | `/global/spend/tags` | 태그별 전체 스펄 | `components/networking.tsx` |
| GET | `/global/spend/all_tag_names` | 전 태그 이름 목록 | `components/networking.tsx` |
| GET | `/management/v1/spend_logs/users` | 스펄 로그 사용자 필터 | `useSpendLogUsers.ts` |
| GET | `/management/v1/spend_logs/end_users` | 스펄 로그 end-user 필터 | `useSpendLogEndUsers.ts` |
| POST | `/cost/estimate` | 비용 추정 (멀티) | `use_multi_cost_estimate.ts` |
| POST | `/usage/ai/chat` | AI 채팅 (streaming) | `components/networking.tsx` |

## Health / General / UI Settings

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/health` | 헬스 체크 (기본) | `components/networking.tsx` |
| GET | `/health/latest` | 최신 헬스 체크 | `components/networking.tsx` |
| GET | `/health/services` | 서비스별 헬스 | `components/networking.tsx` |
| GET | `/health/license` | 라이선스 상태 | `components/networking.tsx` |
| POST | `/health/test_connection` | 모델 연결 테스트 | `components/networking.tsx` |
| GET | `/router/settings` | 라우터 설정 | `components/networking.tsx` |
| GET | `/router/fields` | 라우터 필드 정의 | `useRouterFields.ts` |
| GET | `/callback/configs` | 콜백 설정 | `components/networking.tsx` `callbacksConfigsCall` |
| GET | `/get/config/callbacks` | 콜백 설정 (get) | `components/networking.tsx` `getConfigCallbacks` |
| POST | `/config/callback/delete` | 콜백 삭제 | `components/networking.tsx` `callbackCall`(delete) |
| GET | `/get/ui_settings` | UI 설정 | `components/networking.tsx` `uiSettingsCall` |
| PATCH | `/update/ui_settings` | UI 설정 수정 | `components/networking.tsx` |
| GET | `/get/ui_theme_settings` | UI 테마 설정 | `UIThemeSettings.tsx` / `ThemeContext.tsx` |
| PATCH | `/update/ui_theme_settings` | UI 테마 설정 수정 | `UIThemeSettings.tsx` |
| GET | `/get/user_banner` | 사용자 배너 조회 | `components/networking.tsx` `getUserBanner` |
| PATCH | `/update/user_banner` | 사용자 배너 수정 | `components/networking.tsx` `updateUserBanner` |
| GET | `/get/default_team_settings` | 기본 팀 설정 | `components/networking.tsx` |
| PATCH | `/update/default_team_settings` | 기본 팀 설정 수정 | `components/networking.tsx` |
| GET | `/get/mcp_semantic_filter_settings` | MCP 시맨틱 필터 설정 | `components/networking.tsx` |
| PATCH | `/update/mcp_semantic_filter_settings` | MCP 시맨틱 필터 설정 수정 | `components/networking.tsx` |
| GET | `/get/allowed_ips` | 허용 IP 목록 | `components/networking.tsx` `getAllowedIPs` |
| POST | `/add/allowed_ip` | 허용 IP 추가 | `components/networking.tsx` `addAllowedIP` |
| POST | `/delete/allowed_ip` | 허용 IP 삭제 | `components/networking.tsx` `deleteAllowedIP` |
| GET | `/get_favicon` | 프록시 파비콘 | `app/layout.tsx` (metadata icon) |

## Config

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/config/list?config_type=...` | 설정 목록 (타입별) | `useProxyConfig.ts` |
| GET | `/config/field/info` | 설정 필드 정보 | `components/networking.tsx` |
| POST | `/config/field/update` | 설정 필드 업데이트 | `components/networking.tsx` / `useStoreModelInDB.ts` |
| POST | `/config/field/delete` | 설정 필드 삭제 | `components/networking.tsx` / `useProxyConfig.ts` |
| POST | `/config/update` | 전체 설정 업데이트 | `components/networking.tsx` / `useStoreRequestInSpendLogs.ts` |
| GET | `/config/pass_through_endpoint` | 패스스루 엔드포인트 목록 | `components/networking.tsx` `getPassThroughEndpointsCall` |
| GET | `/config/pass_through_endpoint/team/{teamId}` | 팀별 패스스루 목록 | `components/networking.tsx` |
| POST | `/config/pass_through_endpoint` | 패스스루 엔드포인트 생성 | `components/networking.tsx` |
| POST | `/config/pass_through_endpoint/{endpointPath}` | 특정 엔드포인트 설정 | `components/networking.tsx` |
| DELETE | `/config/pass_through_endpoint?endpoint_id={endpointId}` | 패스스루 엔드포인트 삭제 | `components/networking.tsx` |
| GET, PATCH | `/config/cost_discount_config` | 할인 설정 조회/수정 | `use_discount_config.ts` |
| GET, PATCH | `/config/cost_margin_config` | 마진 설정 조회/수정 | `use_margin_config.ts` |

## Callbacks / Config Extras

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/callbacks/configs` | 콜백 설정 | `components/networking.tsx` |
| POST | `/config/callback/delete` (위 Health/General 섹션 참조) | 콜백 삭제 | — |
| GET | `/coordination_redis/settings` | Redis 설정 조회 | `components/networking.tsx` `getCoordinationRedisSettingsCall` |
| POST | `/coordination_redis/settings` | Redis 설정 저장 | `components/networking.tsx` `updateCoordinationRedisSettingsCall` |
| POST | `/coordination_redis/settings/test` | Redis 연결 테스트 | `components/networking.tsx` `testCoordinationRedisSettingsCall` |

## Cache

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/cache/ping` | 캐시 ping | `components/networking.tsx` `cacheInfoCall`(ping) |
| GET | `/cache/settings` | 캐시 설정 | `components/networking.tsx` `cacheSettingsCall` |
| POST | `/cache/settings` | 캐시 설정 저장 | `components/networking.tsx` `cacheSettingsUpdateCall` |
| POST | `/cache/settings/test` | 캐시 설정 테스트 | `components/networking.tsx` `cacheSettingsTestCall` |

## Email

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/email/event_settings` | 이메일 이벤트 설정 조회 | `components/networking.tsx` `getEmailEventSettings` |
| POST | `/email/event_settings` | 이메일 이벤트 설정 저장 | `components/networking.tsx` `updateEmailEventSettings` |
| POST | `/email/event_settings/reset` | 이메일 이벤트 설정 초기화 | `components/networking.tsx` `resetEmailEventSettings` |

## Vector Store / Indexes / RAG

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/vector_store/list` | 벡터스토어 목록 | `components/networking.tsx` |
| POST | `/vector_store/new` | 벡터스토어 생성 | `components/networking.tsx` |
| POST | `/vector_store/update` | 벡터스토어 수정 | `components/networking.tsx` |
| POST | `/vector_store/info` | 벡터스토어 정보 | `components/networking.tsx` |
| POST | `/vector_store/delete` | 벡터스토어 삭제 | `components/networking.tsx` |
| GET | `/v1/indexes` | 인덱스 목록 | `components/networking.tsx` `indexesListCall` |
| POST | `/v1/indexes` | 인덱스 생성 | `components/networking.tsx` `indexesCreateCall` |
| POST | `/rag/ingest` | RAG 문서 등록 | `components/networking.tsx` |

## Model Hub / Agent Hub / MCP Hub / Skill Hub (공개)

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/public/model_hub` | 모델 허브 목록 | `components/networking.tsx` |
| GET | `/public/model_hub/info` | 모델 허브 상세 | `components/networking.tsx` `fetchModelHubInfo` |
| GET | `/public/agent_hub` | 에이전트 허브 목록 | `components/networking.tsx` |
| GET | `/public/mcp_hub` | MCP 허브 목록 | `components/networking.tsx` |
| GET | `/public/skill_hub` | 스킬 허브 목록 | `components/networking.tsx` |
| GET | `/openapi.json` | 프록시 OpenAPI 스펙 | `components/networking.tsx` |

## Agent / A2A

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/v1/agents` | 에이전트 목록 | `components/networking.tsx` `agentsListCall` / `fetch_agents.tsx` |
| POST | `/v1/agents` | 에이전트 생성 | `components/networking.tsx` `agentCreateCall` |
| PUT | `/v1/agents` | 에이전트 수정 | `components/networking.tsx` `agentUpdateCall` |
| GET | `/v1/agents/{agentId}` | 에이전트 상세 | `components/networking.tsx` `getAgentInfo` |
| PATCH | `/v1/agents/{agentId}` | 에이전트 패치 | `components/networking.tsx` `agentPatchCall` |
| DELETE | `/v1/agents/{agentId}` | 에이전트 삭제 | `components/networking.tsx` `deleteAgentCall` |
| POST | `/v1/agents/make_public` | 에이전트 공개 전환 | `components/networking.tsx` |
| POST | `/v1/a2a/discover` | A2A agent card discover | `components/networking.tsx` `discoverAgentCard` |
| POST | `/a2a/{agentId}/message/send` | A2A 메시지 전송 (JSON-RPC) | `a2a_send_message.tsx` |
| POST | `/a2a/{agentId}` | A2A 메시지 스트림 (JSON-RPC) | `a2a_send_message.tsx` |
| GET | `/agent/daily/activity` | 에이전트 일별 활동 (분 페이지) | `components/networking.tsx` `agentDailyActivityCall` |

## MCP / Tool

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/v1/mcp/access_groups` | MCP 접근 그룹 | `components/networking.tsx` |
| GET | `/v1/mcp/discover` | MCP 서버 Discover | `components/networking.tsx` |
| POST | `/v1/mcp/make_public` | MCP 서버 공개 전환 | `components/networking.tsx` |
| GET | `/v1/mcp/server` | MCP 서버 목록 | `components/networking.tsx` |
| POST | `/v1/mcp/server` | MCP 서버 생성 | `components/networking.tsx` |
| PUT | `/v1/mcp/server` | MCP 서버 수정 | `components/networking.tsx` |
| DELETE | `/v1/mcp/server/{serverId}` | MCP 서버 삭제 | `components/networking.tsx` |
| PUT | `/v1/mcp/server/{serverId}/approve` | MCP 서버 Approve | `components/networking.tsx` |
| PUT | `/v1/mcp/server/{serverId}/reject` | MCP 서버 Reject | `components/networking.tsx` |
| POST | `/v1/mcp/server/register` | MCP 서버 로컬 등록 | `components/networking.tsx` |
| GET | `/v1/mcp/server/health` | MCP 서버 헬스 | `components/networking.tsx` |
| GET | `/v1/mcp/server/submissions` | MCP 서버 제출 목록 | `components/networking.tsx` |
| POST | `/v1/mcp/server/oauth/session` | MCP OAuth 세션 시작 | `components/networking.tsx` |
| POST | `/v1/mcp/server/{serverId}/user-credential` | MCP 사용자 자격 증명 저장 | `ByokCredentialModal.tsx` (`fetchClient`) |
| DELETE, POST | `/v1/mcp/server/{serverId}/oauth-user-credential` | MCP OAuth 사용자 자격 증명 삭제/저장 | `components/networking.tsx` |
| GET | `/v1/mcp/server/{serverId}/oauth-user-credential/status` | OAuth 사용자 자격 증명 상태 | `components/networking.tsx` |
| GET | `/v1/mcp/server/{serverId}/user-env-vars` | MCP 사용자 env vars 조회 | `components/networking.tsx` |
| POST | `/v1/mcp/server/{serverId}/user-env-vars` | MCP 사용자 env vars 저장 | `components/networking.tsx` |
| GET | `/v1/mcp/user-env-vars/status` | 사용자 env vars 전체 상태 | `components/networking.tsx` |
| GET | `/v1/mcp/user-credentials` | 사용자 MCP 자격 증명 목록 | `components/networking.tsx` |
| GET | `/v1/mcp/openapi-registry` | MCP OpenAPI 레지스트리 | `components/networking.tsx` |
| GET | `/v1/mcp/network/client-ip` | MCP 클라이언트 IP 조회 | `components/networking.tsx` |
| GET | `/v1/mcp/toolset` | MCP toolset 목록 | `components/networking.tsx` |
| POST | `/v1/mcp/toolset` | MCP toolset 생성 | `components/networking.tsx` |
| PUT | `/v1/mcp/toolset` | MCP toolset 수정 | `components/networking.tsx` |
| DELETE | `/v1/mcp/toolset/{toolsetId}` | MCP toolset 삭제 | `components/networking.tsx` |
| GET | `/mcp-rest/tools/list?server_id=...` | MCP REST 도구 목록 | `components/networking.tsx` |
| POST | `/mcp-rest/tools/call` | MCP REST 도구 호출 | `components/networking.tsx` |
| POST | `/mcp-rest/test/tools/list` | MCP REST 도구 목록(테스트) | `components/networking.tsx` |

## Tool (프록시 tool)

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/v1/tool/list` | 프록시 tool 목록 | `components/networking.tsx` |
| POST | `/v1/tool/policy` | tool 정책 적용 | `components/networking.tsx` |
| GET | `/v1/tool/{toolName}/detail` | tool 상세 | `components/networking.tsx` |
| GET | `/v1/tool/{toolName}/logs` | tool 로그 | `components/networking.tsx` |
| DELETE | `/v1/tool/{toolName}/overrides` | tool 오버라이드 삭제 | `components/networking.tsx` |
| GET | `/v1/tool/spend` | tool별 스펄 | `components/networking.tsx` |
| GET | `/public/providers/fields` | 프로바이더 필드 | `components/networking.tsx` |
| GET | `/public/agents/fields` | 에이전트 필드 | `components/networking.tsx` |
| GET | `/public/litellm_model_cost_map` | LiteLLM 모델 비용표 | `components/networking.tsx` |
| GET | `/public/complexity_router/scorer_defaults` | 복잡도 라우터 스코어 기본값 | `components/networking.tsx` |

## Search Tool

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/search_tools/list` | 검색 도구 목록 | `components/networking.tsx` |
| POST | `/search_tools` | 검색 도구 생성 | `components/networking.tsx` |
| PUT | `/search_tools/{searchToolId}` | 검색 도구 수정 | `components/networking.tsx` |
| DELETE | `/search_tools/{searchToolId}` | 검색 도구 삭제 | `components/networking.tsx` |
| POST | `/search_tools/test_connection` | 검색 도구 연결 테스트 | `components/networking.tsx` |
| GET | `/search_tools/ui/available_providers` | 검색 도구 사용 가능 프로바이더 | `components/networking.tsx` |

## Prompt

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/prompts/list` | 프롬프트 목록 | `components/networking.tsx` |
| POST | `/prompts` | 프롬프트 생성 | `components/networking.tsx` |
| PUT | `/prompts/{promptId}` | 프롬프트 수정 | `components/networking.tsx` |
| DELETE | `/prompts/{promptId}` | 프롬프트 삭제 | `components/networking.tsx` |
| GET | `/prompts/{promptId}/info` | 프롬프트 정보 | `components/networking.tsx` |
| GET | `/prompts/{promptId}/versions` | 프롬프트 버전 목록 | `components/networking.tsx` |
| POST | `/utils/dotprompt_json_converter` | .dotprompt JSON 변환 | `components/networking.tsx` |

## Access Group

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/v1/access_group` | 접근 그룹 목록 | `useAccessGroups.ts` (raw fetch) |
| POST | `/v1/access_group` | 접근 그룹 생성 | `AccessGroupCreateDialog.tsx` (`fetchClient`) |
| PUT | `/v1/access_group/{accessGroupId}` | 접근 그룹 수정 | `useEditAccessGroup.ts` |
| DELETE | `/v1/access_group/{accessGroupId}` | 접근 그룹 삭제 | `useDeleteAccessGroup.ts` |

## Memory

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/v1/memory` | 메모리 목록 | `components/networking.tsx` `memoryListCall` |
| POST | `/v1/memory` | 메모리 저장 | `components/networking.tsx` `memoryCreateCall` |
| PUT | `/v1/memory/{key}` | 메모리 갱신 | `components/networking.tsx` `memoryUpdateCall` |
| DELETE | `/v1/memory/{key}` | 메모리 삭제 | `components/networking.tsx` `memoryDeleteCall` |

## Audit / Invitation / Compliance / CloudZero

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/audit` | 감사 로그 | `components/networking.tsx` `getAuditLogs` |
| GET | `/alerting/settings` | 알림 설정 | `components/networking.tsx` |
| POST | `/invitation/new` | 초대 생성 | `components/networking.tsx` |
| GET | `/onboarding/get_token` | 온보딩 토큠큰 (공개) | `components/networking.tsx` `getOnboardingToken` |
| POST | `/onboarding/claim_token` | 온보딩 토큰 claim | `components/networking.tsx` `claimOnboardingToken` |
| POST | `/compliance/eu-ai-act` | EU AI Act 준수 | `components/networking.tsx` |
| POST | `/compliance/gdpr` | GDPR 준수 | `components/networking.tsx` |
| GET | `/cloudzero/settings` | CloudZero 설정 | `useCloudZeroSettings.ts` |
| PUT | `/cloudzero/settings` | CloudZero 설정 수정 | `cloudzero_export_modal.tsx` |
| POST | `/cloudzero/settings` | CloudZero 설정 생성 | `cloudzero_export_modal.tsx` |
| DELETE | `/cloudzero/delete` | CloudZero 설정 삭제 | `useCloudZeroSettings.ts` |
| POST | `/cloudzero/init` | CloudZero 초기화 | `useCloudZeroCreate.ts` |
| POST | `/cloudzero/export` | CloudZero 내보내기 | `useCloudZeroExport.ts` |
| POST | `/cloudzero/dry-run` | CloudZero dry-run | `useCloudZeroDryRun.ts` |

## Config Overrides

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/config_overrides/hashicorp_vault` | Vault override 조회 | `hashicorpVaultApi.ts` |
| POST | `/config_overrides/hashicorp_vault` | Vault override 저장 | `hashicorpVaultApi.ts` |
| DELETE | `/config_overrides/hashicorp_vault` | Vault override 삭제 | `hashicorpVaultApi.ts` |
| POST | `/config_overrides/hashicorp_vault/test_connection` | Vault 연결 테스트 | `hashicorpVaultApi.ts` |

## SCIM

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/scim/v2/...` | SCIM v2 표준 (Users/Groups/ResourceTypes/Schemas/ServiceProviderConfig) | `components/SCIM.tsx` / `key_scope.ts` |

## LLM (플레이그라운드로부터 대외 호출)

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| POST | `/v1/chat/completions` | Chat Completions | `chatConstants.ts` |
| POST | `/v1/responses` | Responses API | `chatConstants.ts` |
| POST | `/v1/messages` | Anthropic Messages | `chatConstants.ts` |
| POST | `/v1/images/generations` | 이미지 생성 | `chatConstants.ts` |
| POST | `/v1/images/edits` | 이미지 편집 | `chatConstants.ts` |
| POST | `/v1/embeddings` | 임베딩 | `chatConstants.ts` |
| POST | `/v1/audio/speech` | TTS | `chatConstants.ts` |
| POST | `/v1/audio/transcriptions` | STT | `chatConstants.ts` |
| POST | `/v1/realtime` | 실시간 오디오 | `chatConstants.ts` |
| POST | `/v1beta/interactions` | Vertex interactions passthrough | `chatConstants.ts` |
| POST | `/mcp-rest/tools/call` | MCP REST 도구 호출 (플레이그라운드) | `chatConstants.ts` |
| POST | `/v1/a2a/message/send` | A2A 메시지 (플레이그라운드 라벨) | `chatConstants.ts` |

## Plugin

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/api/plugins` | 플러그인 목록 | `PluginModeContext.tsx` (`pluginApiClient`) |
| GET | `/api/plugins/auth-token?plugin_name=...` | 플러그인 인증 토큰 | `app/(dashboard)/layout.tsx` |

## Claude Code

| Method | Path | 용도 | 출처 |
|---|---|---|---|
| GET | `/claude-code/marketplace.json` | 마켓플레이스 목록 | `components/networking.tsx` |
| GET | `/claude-code/plugins` | 플러그인 목록 | `components/networking.tsx` `getClaudeCodePlugins` |
| GET | `/claude-code/plugins/{pluginName}` | 플러그인 상세 | `components/networking.tsx` |
| POST | `/claude-code/plugins` | 플러그인 설치 | `components/networking.tsx` `createClaudeCodePlugin` |
| DELETE | `/claude-code/plugins/{pluginName}` | 플러그인 삭제 | `components/networking.tsx` |
| POST | `/claude-code/plugins/{pluginName}/enable` | 플러그인 활성화 | `components/networking.tsx` |
| POST | `/claude-code/plugins/{pluginName}/disable` | 플러그인 비활성화 | `components/networking.tsx` |
