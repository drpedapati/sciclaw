// API client for sciClaw web backend
const BASE = '/api';

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...opts?.headers },
    ...opts,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || res.statusText);
  }
  return res.json();
}

// ── Snapshot (polled every 10s, same as TUI) ──

// Go structs serialize as PascalCase (no json tags)
export interface ChannelSnapshot {
  Status: string;       // ready | open | broken | off
  Enabled: boolean;
  HasToken: boolean;
  ApprovedUsers: { UserID: string; Username: string; Raw: string }[];
}

export interface Snapshot {
  // System
  State: string;
  IPv4: string;
  Load: string;
  Memory: string;
  // Config
  ConfigExists: boolean;
  WorkspacePath: string;
  AuthStoreExists: boolean;
  // Providers
  OpenAI: string;    // ready | missing
  Anthropic: string; // ready | missing
  // Active model
  ActiveModel: string;
  ActiveProvider: string;
  // Channels
  Discord: ChannelSnapshot;
  Telegram: ChannelSnapshot;
  // Service
  ServiceInstalled: boolean;
  ServiceRunning: boolean;
  ServiceAutoStart: boolean;
}

export const getSnapshot = () => request<Snapshot>('/snapshot');

// ── Home ──
export interface SetupChecklist {
  config: boolean;
  auth: boolean;
  channel: boolean;
  service: boolean;
}
export const getChecklist = () => request<SetupChecklist>('/home/checklist');
export const runOnboard = () => request<{ ok: boolean }>('/home/onboard', { method: 'POST' });
export const runSmokeTest = (model?: string) =>
  request<{ output: string; ok: boolean }>('/home/smoke-test', {
    method: 'POST',
    body: JSON.stringify({ model }),
  });

// ── Chat ──
export const sendChat = (message: string) =>
  request<{ response: string }>('/chat', {
    method: 'POST',
    body: JSON.stringify({ message }),
  });

// ── Channels ──
export const addChannelUser = (channel: string, userId: string, name: string) =>
  request<{ ok: boolean }>(`/channels/${channel}/users`, {
    method: 'POST',
    body: JSON.stringify({ userId, name }),
  });
export const removeChannelUser = (channel: string, userId: string) =>
  request<{ ok: boolean }>(`/channels/${channel}/users/${userId}`, { method: 'DELETE' });
export const setupChannel = (channel: string, token: string, userId: string, name: string) =>
  request<{ ok: boolean }>(`/channels/${channel}/setup`, {
    method: 'POST',
    body: JSON.stringify({ token, userId, name }),
  });

// ── Email ──
export interface EmailConfig {
  enabled: boolean;
  provider: string;
  address: string;
  displayName: string;
  hasApiKey: boolean;
  baseUrl: string;
  allowFrom: string[];
}
export const getEmailConfig = () => request<EmailConfig>('/email');
export const updateEmailConfig = (data: Partial<EmailConfig & { apiKey: string }>) =>
  request<{ ok: boolean }>('/email', { method: 'PUT', body: JSON.stringify(data) });
export const sendTestEmail = (to: string) =>
  request<{ ok: boolean; output: string }>('/email/test', {
    method: 'POST',
    body: JSON.stringify({ to }),
  });

// ── Login / Auth ──
export interface AuthStatus {
  provider: string;
  status: string;  // active | not_set
  method: string;
}
export const getAuthStatus = () => request<AuthStatus[]>('/auth');
export const loginProvider = (provider: string) =>
  request<{ ok: boolean; output: string }>(`/auth/${provider}/login`, { method: 'POST' });
export const logoutProvider = (provider: string) =>
  request<{ ok: boolean }>(`/auth/${provider}/logout`, { method: 'POST' });
export const setApiKey = (provider: string, key: string) =>
  request<{ ok: boolean }>(`/auth/${provider}/key`, {
    method: 'POST',
    body: JSON.stringify({ key }),
  });

// ── Doctor ──
export interface DoctorReport {
  version: string;
  os: string;
  arch: string;
  timestamp: string;
  checks: { name: string; status: string; message: string }[];
  passed: number;
  warnings: number;
  errors: number;
  skipped: number;
}
export const runDoctor = () => request<DoctorReport>('/doctor', { method: 'POST' });

// ── Gateway / Service ──
export const serviceAction = (action: string) =>
  request<{ ok: boolean; output: string }>(`/service/${action}`, { method: 'POST' });
export const getServiceLogs = () => request<{ logs: string }>('/service/logs');

// ── Jobs ──
export interface JobsSummary {
  total: number;
  active: number;
  running: number;
  queued: number;
  done: number;
  failed: number;
  interrupted: number;
  cancelled: number;
  distinctChannels: number;
  distinctChats: number;
  distinctUsers: number;
  distinctWorkspaces: number;
}

export interface JobRecord {
  id: string;
  shortId: string;
  channel: string;
  chatId: string;
  workspace: string;
  routeLabel: string;
  runtimeKey: string;
  targetKey: string;
  class: string;
  lane: string;
  state: string;
  phase: string;
  detail: string;
  summary: string;
  askSummary: string;
  lastError: string;
  statusMessageId: string;
  userId: string;
  userName: string;
  messageId: string;
  sessionKey: string;
  startedAt: number;
  updatedAt: number;
  durationSec: number;
  stale: boolean;
}

export interface JobsResponse {
  generatedAt: number;
  summary: JobsSummary;
  jobs: JobRecord[];
}

export const getJobs = () => request<JobsResponse>('/jobs');

// ── Models ──
export interface ModelInfo {
  current: string;
  provider: string;
  effort: string;
  authMethod: string;
}
export interface ModelCatalogEntry {
  id: string;
  name: string;
  provider: string;
  source: string;
}
export interface ModelCatalogResponse {
  provider: string;
  source: string;
  warning?: string;
  models: ModelCatalogEntry[];
}
export const getModelInfo = () => request<ModelInfo>('/models');
export const getModelCatalog = () => request<ModelCatalogResponse>('/models/catalog');
export interface SetModelResponse {
  ok: boolean;
  model: string;
  provider: string;
  restartRequired: boolean;
}
export const setModel = (model: string) =>
  request<SetModelResponse>('/models', { method: 'PUT', body: JSON.stringify({ model }) });
export const setEffort = (effort: string) =>
  request<{ ok: boolean }>('/models/effort', { method: 'PUT', body: JSON.stringify({ effort }) });

// ── PHI ──
export interface PhiData {
  mode: string;
  cloudModel: string;
  cloudProvider: string;
  localBackend: string;
  localModel: string;
  localPreset: string;
  backendRunning: boolean;
  backendInstalled: boolean;
  backendVersion: string;
  modelReady: boolean;
  hardware: string;
  lastEval: string;
  probeStatus: string;
}
export const getPhiData = () => request<PhiData>('/phi');
export const phiAction = (action: string, data?: Record<string, string>) =>
  request<{ ok: boolean; output: string }>(`/phi/${action}`, {
    method: 'POST',
    body: JSON.stringify(data || {}),
  });

// ── Skills ──
export interface Skill {
  name: string;
  source: string;  // workspace | builtin | global
  description: string;
  status: string;
}
export const getSkills = () => request<Skill[]>('/skills');
export const installSkill = (path: string) =>
  request<{ ok: boolean }>('/skills', { method: 'POST', body: JSON.stringify({ path }) });
export const removeSkill = (name: string) =>
  request<{ ok: boolean }>(`/skills/${name}`, { method: 'DELETE' });

// ── Schedule ──
export interface CronJob {
  id: string;
  name: string;
  schedule: string;
  enabled: boolean;
  nextRun: string;
}
export const getCronJobs = () => request<CronJob[]>('/cron');
export const addCronJob = (description: string) =>
  request<{ ok: boolean }>('/cron', { method: 'POST', body: JSON.stringify({ description }) });
export const toggleCronJob = (id: string) =>
  request<{ ok: boolean }>(`/cron/${id}/toggle`, { method: 'POST' });
export const removeCronJob = (id: string) =>
  request<{ ok: boolean }>(`/cron/${id}`, { method: 'DELETE' });

// ── Routing ──
export interface RoutingStatus {
  enabled: boolean;
  unmappedBehavior: string;
  totalMappings: number;
  invalidMappings: number;
}
export interface RoutingMapping {
  id: string;
  channel: string;
  chatId: string;
  workspace: string;
  allowedSenders: string[];
  label: string;
  mode: string;
  localBackend: string;
  localModel: string;
  localPreset: string;
}
export const getRoutingStatus = () => request<RoutingStatus>('/routing/status');
export const getRoutingMappings = () => request<RoutingMapping[]>('/routing/mappings');
export const addRoutingMapping = (data: Partial<RoutingMapping>) =>
  request<{ ok: boolean }>('/routing/mappings', { method: 'POST', body: JSON.stringify(data) });
export const removeRoutingMapping = (id: string) =>
  request<{ ok: boolean }>(`/routing/mappings/${id}`, { method: 'DELETE' });
export const routingReload = () =>
  request<{ ok: boolean }>('/routing/reload', { method: 'POST' });

// ── Settings ──
export interface Settings {
  discord: { enabled: boolean };
  telegram: { enabled: boolean };
  routing: { enabled: boolean; unmappedBehavior: string };
  agent: { defaultModel: string; reasoningEffort: string };
  integrations: { pubmedApiKey: string };
  service: { autoStart: boolean; installed: boolean; running: boolean };
  general: { workspacePath: string };
}
export const getSettings = () => request<Settings>('/settings');
export const updateSetting = (path: string, value: unknown) =>
  request<{ ok: boolean }>('/settings', {
    method: 'PUT',
    body: JSON.stringify({ path, value }),
  });
