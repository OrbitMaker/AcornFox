// walk-targets.mjs 的类型（给单元测试 src/lib/walk-targets.test.ts 用；走查脚本本身是 node 直接跑的 .mjs）。
export const DEFAULT_APPS: string
export const DEFAULT_SET: string
export const DEFAULT_NEEDS_TIMEOUT_MS: number
export const MASKED: string

export interface WalkSet { app: string; key: string; value: string }
export interface WalkTargets { apps: string[]; recipe: string; sets: WalkSet[]; needsTimeout: number }
export function walkTargets(env?: Record<string, string | undefined>): WalkTargets

// 只用到 GET /v1/catalog 应答里的这几项。
export interface CatalogLike {
  apps?: { id: string; name?: string; uses?: { name?: string }[] | null }[] | null
  recipes?: { id: string; title?: string; apps?: { app: string }[] | null }[] | null
}
export function recipeApps(cat: CatalogLike | null | undefined, id: string): string[]
export function declaredNeeds(cat: CatalogLike | null | undefined, kind: 'app' | 'recipe', id: string): string[]
export function needsVerdict(declared: string[], statuses: string[]): { ok: boolean | null; detail: string }

export interface FirstLoginLike { fields?: { label?: string; value?: string; secret?: boolean }[] | null }
export function firstLoginResponse(status: number, body: unknown): { fl: FirstLoginLike | null; error: string }
export function loginExpectation(fl: FirstLoginLike | null | undefined): { shown: boolean; secret: boolean; maskedByServer: boolean }

// 下面几个要操作 Playwright 的页面，单元测试不用。
export function catalogNames(page: unknown): Promise<{
  needs: (kind: 'app' | 'recipe', id: string) => string[]
  recipeApps: (id: string) => string[]
  app: (id: string) => string
  recipe: (id: string) => string
}>
export function fillSettings(dialog: unknown, sets: WalkSet[], forApp: string): Promise<void>
export function readNeedStatuses(dialog: unknown, want: number, timeout: number): Promise<string[]>
export function fetchFirstLogin(page: unknown, app: string): Promise<{ fl: FirstLoginLike | null; error: string }>
export function checkFirstLogin(page: unknown, dialog: unknown, app: string): Promise<null | {
  shown: { ok: boolean; detail: string }
  masked: { ok: boolean; detail: string } | null
}>
