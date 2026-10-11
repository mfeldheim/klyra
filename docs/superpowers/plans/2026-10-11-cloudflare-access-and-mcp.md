# klyra: Cloudflare Access (Entra) + MCP server, remove AI features

Status: in progress (2026-10-11)

## Goal
- Replace oauth2-proxy (GitHub OAuth) with **Cloudflare Access + Microsoft Entra ID** in front of `klyra.opendi.com`.
- Add a **remote MCP server** to klyra (`POST /mcp`, Streamable HTTP JSON-RPC) exposing read-only diagnostic tools so Claude can investigate incidents.
- **Remove the AI features** (Bedrock investigate agent, chat, SMS investigation UI). Claude via MCP replaces them.
- Keep incident logging (S3 `klyra-incidents`) and alerting (Pushover).

## Decisions (approved)
1. Cloudflare Access app managed **in Terraform**.
2. MCP reached as **remote MCP behind CF Access + service token**.
3. MCP tools = existing 17 K8s read-only tools **+ klyra-native** (status/monitors/incidents/silences/history).
4. Change **both repos**, commit + push to `main`.

## Repo A: klyra (this repo)
- [ ] New package `internal/diagnostics`: move K8s tools out of investigate; export `ToolDef`, `ToolResult`, `K8sTools`, `Definitions()`, `Execute()`.
- [ ] New package `internal/mcp`: JSON-RPC 2.0 Streamable HTTP handler: `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`.
- [ ] MCP tools: 17 K8s + klyra-native (status, list_monitors, list_silences, list_incidents, get_incident, history).
- [ ] `internal/server`: mount `/mcp`; remove `/api/incidents/{id}/stream` and `/chat`; GetIncident returns metadata + content; `/api/me` reads `Cf-Access-Authenticated-User-Email`.
- [ ] `internal/incident`: drop `InvestigationStatus`, `Conv*`, `InvRunner`, `RunInvestigation`, `Chat`, `Subscribe/Unsubscribe`, `updateStatus`.
- [ ] `cmd/root.go`: drop Bedrock/investigate wiring; wire MCP handler.
- [ ] `internal/config/types.go`: drop `AIInvestigateConfig`.
- [ ] UI: Incident page becomes read-only (no SSE/chat); remove `chatIncident`; sign-out → `/cdn-cgi/access/logout`.
- [ ] RBAC: rename `-investigate` → `-read` (read-only tools, used by MCP).
- [ ] `go mod tidy`; `go build`; `go test ./...`.
- [ ] Commit + push to main. Record short SHA for image tag.

## Repo B: terraform-infra
- [ ] `argocd/.../klyra/kustomization.yaml`: remove oauth2-proxy helmChart + patch; bump klyra image tag to new sha.
- [ ] `argocd/.../klyra/values.yaml`: remove `ai-investigate` action.
- [ ] `argocd/.../klyra/gateway.yaml`: HTTPRoute → Service `klyra:8080`.
- [ ] delete `oauth2-proxy-values.yaml` (+ `charts/oauth2-proxy-10.4.3`).
- [ ] `terraform/infra_cloudflare_klyra_access.tf`: `cloudflare_zero_trust_access_application` (klyra.opendi.com) + reusable policies + `cloudflare_zero_trust_access_service_token` for MCP.
- [ ] `terraform/infra_klyra.tf`: remove `klyra_bedrock` IAM policy.
- [ ] `terraform fmt`; commit + push to main.

## Notes
- Image tag strategy: use CI-built short-sha tag `ghcr.io/mfeldheim/klyra:<sha>` (pullPolicy IfNotPresent).
- No Helm chart version bump required unless chart files change (RBAC rename requires chart release — preferred: keep chart RBAC name as-is to avoid an OCI chart release, OR tag v0.2.10). **Decision: keep chart RBAC name `-investigate` unchanged** to avoid a chart release; revisit later.
