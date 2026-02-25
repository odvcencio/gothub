const BASE = '/api/v1';

let token: string | null = localStorage.getItem('gothub_token');
let redirectingToLogin = false;

export function setToken(t: string | null) {
  token = t;
  if (t) localStorage.setItem('gothub_token', t);
  else localStorage.removeItem('gothub_token');
}

export function getToken() { return token; }

function isAuthRequest(path: string): boolean {
  return path.startsWith('/auth/');
}

function buildLoginRedirectPath(): string {
  if (typeof window === 'undefined') return '/login';

  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  const returnTo = window.location.pathname.startsWith('/login') ? '/' : current || '/';
  const params = new URLSearchParams({ session: 'expired' });

  if (returnTo && returnTo !== '/login') {
    params.set('returnTo', returnTo);
  }

  return `/login?${params.toString()}`;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const resp = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (resp.status === 401 && !isAuthRequest(path)) {
    // Unauthorized: clear auth state and send user back to sign-in.
    setToken(null);
    if (typeof window !== 'undefined' && !redirectingToLogin && !window.location.pathname.startsWith('/login')) {
      redirectingToLogin = true;
      window.location.assign(buildLoginRedirectPath());
    }
    throw new Error('authentication required');
  }

  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || resp.statusText);
  }

  if (resp.status === 204) return undefined as T;
  return resp.json();
}

export interface APIUser {
  id: number;
  username: string;
  email?: string;
  [key: string]: unknown;
}

export interface AuthResponse {
  token: string;
  user: APIUser;
}

export interface AuthCapabilities {
  password_auth_enabled: boolean;
  magic_link_enabled: boolean;
  ssh_auth_enabled: boolean;
  passkey_enabled: boolean;
}

export interface Notification {
  id: number;
  read_at?: string;
  created_at?: string;
  [key: string]: unknown;
}

export interface Repository {
  id: number;
  name: string;
  description?: string;
  is_private: boolean;
  default_branch?: string;
  owner_name?: string;
  parent_repo_id?: number;
  parent_owner?: string;
  parent_name?: string;
  [key: string]: unknown;
}

export interface RepoStars {
  count: number;
  starred: boolean;
}

export interface TreeEntry {
  name: string;
  is_dir: boolean;
  blob_hash?: string;
  subtree_hash?: string;
  [key: string]: unknown;
}

export interface BlobResponse {
  data?: string;
  [key: string]: unknown;
}

export interface CommitSummary {
  hash: string;
  author?: string;
  message?: string;
  timestamp?: number | string;
  [key: string]: unknown;
}

export interface RepoIndexStatus {
  ref: string;
  commit_hash: string;
  indexed: boolean;
  queue_status: 'queued' | 'in_progress' | 'failed' | 'completed' | 'not_found' | string;
  attempts: number;
  last_error?: string;
  updated_at: string;
}

export interface EntityDescriptor {
  key: string;
  kind?: string;
  decl_kind?: string;
  name?: string;
  [key: string]: unknown;
}

export interface FileEntity {
  key: string;
  kind: string;
  name: string;
  decl_kind: string;
  receiver?: string;
  signature?: string;
  start_line: number;
  end_line: number;
  body_hash: string;
  [key: string]: unknown;
}

export interface EntityListResponse {
  language: string;
  path: string;
  entities: FileEntity[];
}

export interface EntityLogHit {
  commit_hash: string;
  author: string;
  timestamp: number;
  message: string;
  path?: string;
  key: string;
}

export interface EntityBlameInfo {
  commit_hash: string;
  author: string;
  timestamp: number;
  message: string;
  path?: string;
  key: string;
}

export interface DiffFileChange {
  type: string;
  key: string;
  before?: { name?: string; decl_kind?: string };
  after?: { name?: string; decl_kind?: string };
}

export interface DiffFile {
  path: string;
  changes: DiffFileChange[];
}

export interface PullRequest {
  id: number;
  number: number;
  title: string;
  body?: string;
  state: string;
  source_branch: string;
  target_branch: string;
  [key: string]: unknown;
}

export interface MergeGate {
  allowed: boolean;
  reasons?: string[];
  entity_owner_approvals?: EntityOwnerApproval[];
}

export interface CheckRun {
  id?: number;
  name: string;
  status: string;
  conclusion?: string;
  [key: string]: unknown;
}

export interface EntityOwnerApproval {
  path: string;
  entity_key: string;
  required_owners?: string[];
  approved_by?: string[];
  missing_owners?: string[];
  unresolved_teams?: string[];
  satisfied: boolean;
}

export interface PRComment {
  id: number;
  body: string;
  author_name?: string;
  created_at?: string;
  [key: string]: unknown;
}

export interface PRReview {
  id: number;
  state: string;
  body?: string;
  author_name?: string;
  created_at?: string;
  [key: string]: unknown;
}

export interface MergePreviewStats {
  total_entities: number;
  unchanged: number;
  ours_modified: number;
  theirs_modified: number;
  both_modified: number;
  added: number;
  deleted: number;
  conflicts: number;
}

export interface MergePreviewFile {
  path: string;
  status: string;
  conflict_count: number;
  entities?: EntityDescriptor[];
}

export interface MergePreviewResponse {
  has_conflicts: boolean;
  conflict_count: number;
  stats: MergePreviewStats;
  files: MergePreviewFile[];
}

export interface BranchProtectionRule {
  required_reviews?: number;
  required_checks?: string[];
  require_entity_owner_approval?: boolean;
  [key: string]: unknown;
}

export interface Webhook {
  id: number;
  url: string;
  events?: string[];
  active?: boolean;
  [key: string]: unknown;
}

export interface WebhookDelivery {
  id: number;
  status?: string;
  response_code?: number;
  created_at?: string;
  [key: string]: unknown;
}

export interface SSHKey {
  id: number;
  name: string;
  public_key: string;
  [key: string]: unknown;
}

export interface Collaborator {
  user_id?: number;
  username: string;
  role: string;
  [key: string]: unknown;
}

export interface Organization {
  id?: number;
  name: string;
  display_name?: string;
  [key: string]: unknown;
}

export interface SymbolResult {
  id?: string;
  name: string;
  kind: string;
  file: string;
  signature?: string;
  receiver?: string;
  start_line?: number;
  end_line?: number;
  [key: string]: unknown;
}

export interface ReferenceResult {
  name: string;
  file: string;
  kind?: string;
  line?: number;
  start_line?: number;
  end_line?: number;
  start_column?: number;
  end_column?: number;
  [key: string]: unknown;
}

export interface CallGraphEdge {
  caller_name: string;
  caller_file: string;
  callee_name: string;
  callee_file: string;
  [key: string]: unknown;
}

export interface CallGraphResponse {
  definitions: SymbolResult[];
  edges: CallGraphEdge[];
}

export interface EntityHistoryHit {
  stable_id: string;
  name: string;
  path: string;
  commit_hash: string;
  author?: string;
  timestamp?: number;
  message?: string;
  entity_hash?: string;
  kind?: string;
  decl_kind?: string;
  receiver?: string;
  body_hash?: string;
  [key: string]: unknown;
}

export interface SemverRecommendation {
  base?: string;
  head?: string;
  bump: string;
  breaking_changes?: string[];
  features?: string[];
  fixes?: string[];
  [key: string]: unknown;
}

export interface Issue {
  id: number;
  number: number;
  title: string;
  body?: string;
  state: 'open' | 'closed';
  author_name?: string;
  [key: string]: unknown;
}

export interface IssueComment {
  id: number;
  body: string;
  author_name?: string;
  created_at?: string;
  [key: string]: unknown;
}

export interface RepoStreamEvent {
  type: string;
  repo_id: number;
  occurred_at?: string;
  payload?: Record<string, unknown>;
}

export interface RepoEventStreamSubscription {
  close: () => void;
}

interface RepoEventStreamHandlers {
  onEvent: (event: RepoStreamEvent) => void;
  onError?: (error: Error) => void;
  onOpen?: () => void;
}

// Auth
export const register = (username: string, email: string, password: string) =>
  request<AuthResponse>('POST', '/auth/register', { username, email, password });
export const login = (username: string, password: string) =>
  request<AuthResponse>('POST', '/auth/login', { username, password });
export const requestMagicLink = (email: string) =>
  request<{ sent: boolean; token?: string; expires_at?: string }>('POST', '/auth/magic/request', { email });
export const verifyMagicLink = (token: string) =>
  request<AuthResponse>('POST', '/auth/magic/verify', { token });
export const beginSSHLogin = (username: string, fingerprint?: string) =>
  request<{ challenge_id: string; challenge: string; fingerprint: string; expires_at: string }>('POST', '/auth/ssh/challenge', { username, fingerprint });
export const finishSSHLogin = (challengeId: string, signature: string, signatureFormat: string) =>
  request<AuthResponse>('POST', '/auth/ssh/verify', { challenge_id: challengeId, signature, signature_format: signatureFormat });
export const beginWebAuthnRegistration = () =>
  request<{ session_id: string; options: Record<string, unknown> }>('POST', '/auth/webauthn/register/begin');
export const finishWebAuthnRegistration = (sessionId: string, credential: Record<string, unknown>) =>
  request<{ credential_id: string }>('POST', '/auth/webauthn/register/finish', { session_id: sessionId, credential });
export const beginWebAuthnLogin = (username: string) =>
  request<{ session_id: string; options: Record<string, unknown> }>('POST', '/auth/webauthn/login/begin', { username });
export const finishWebAuthnLogin = (sessionId: string, credential: Record<string, unknown>) =>
  request<AuthResponse>('POST', '/auth/webauthn/login/finish', { session_id: sessionId, credential });
export const getAuthCapabilities = () =>
  request<AuthCapabilities>('GET', '/auth/capabilities');
export const refreshToken = () =>
  request<AuthResponse>('POST', '/auth/refresh');
export const changePassword = (currentPassword: string, newPassword: string) =>
  request<AuthResponse>('POST', '/auth/change-password', { current_password: currentPassword, new_password: newPassword });
export const getUser = () => request<APIUser>('GET', '/user');
export const listNotifications = (unread?: boolean, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (unread) query.set('unread', 'true');
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<Notification[]>('GET', `/notifications${suffix ? '?' + suffix : ''}`);
};
export const getUnreadNotificationsCount = () => request<{ count: number }>('GET', '/notifications/unread-count');
export const markNotificationRead = (id: number) => request<void>('POST', `/notifications/${id}/read`);
export const markAllNotificationsRead = () => request<void>('POST', '/notifications/read-all');

// Repos
export const createRepo = (name: string, description: string, isPrivate: boolean) =>
  request<Repository>('POST', '/repos', { name, description, private: isPrivate });
export const forkRepo = (owner: string, repo: string, name?: string) =>
  request<Repository>('POST', `/repos/${owner}/${repo}/forks`, name ? { name } : undefined);
export const listRepoForks = (owner: string, repo: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<Repository[]>('GET', `/repos/${owner}/${repo}/forks${suffix ? '?' + suffix : ''}`);
};
export const getRepo = (owner: string, repo: string) =>
  request<Repository>('GET', `/repos/${owner}/${repo}`);
export const listUserRepos = () => request<Repository[]>('GET', '/user/repos');
export const listUserStarredRepos = () => request<Repository[]>('GET', '/user/starred');
export const getRepoStars = (owner: string, repo: string) =>
  request<RepoStars>('GET', `/repos/${owner}/${repo}/stars`);
export const starRepo = (owner: string, repo: string) =>
  request<RepoStars>('PUT', `/repos/${owner}/${repo}/star`);
export const unstarRepo = (owner: string, repo: string) =>
  request<RepoStars>('DELETE', `/repos/${owner}/${repo}/star`);
export const listRepoStargazers = (owner: string, repo: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<APIUser[]>('GET', `/repos/${owner}/${repo}/stargazers${suffix ? '?' + suffix : ''}`);
};

// Browsing
export const listTree = (owner: string, repo: string, ref: string, path?: string) =>
  request<TreeEntry[]>('GET', `/repos/${owner}/${repo}/tree/${ref}${path ? '/' + path : ''}`);
export const listBranches = (owner: string, repo: string) =>
  request<string[]>('GET', `/repos/${owner}/${repo}/branches`);
export const getBlob = (owner: string, repo: string, ref: string, path: string) =>
  request<BlobResponse>('GET', `/repos/${owner}/${repo}/blob/${ref}/${path}`);
export const listCommits = (owner: string, repo: string, ref: string) =>
  request<CommitSummary[]>('GET', `/repos/${owner}/${repo}/commits/${ref}`);
export const getCommit = (owner: string, repo: string, hash: string) =>
  request<CommitSummary>('GET', `/repos/${owner}/${repo}/commit/${hash}`);
export const getRepoIndexStatus = (owner: string, repo: string, ref: string) =>
  request<RepoIndexStatus>('GET', `/repos/${owner}/${repo}/index/status?ref=${encodeURIComponent(ref)}`);

// Entities & Diff
export const listEntities = (owner: string, repo: string, ref: string, path: string) =>
  request<EntityListResponse>('GET', `/repos/${owner}/${repo}/entities/${ref}/${path}`);
export const getEntityLog = (owner: string, repo: string, ref: string, key: string, path?: string, limit?: number) => {
  const params = new URLSearchParams({ key });
  if (path) params.set('path', path);
  if (limit && limit > 0) params.set('limit', String(limit));
  return request<EntityLogHit[]>('GET', `/repos/${owner}/${repo}/entity-log/${ref}?${params.toString()}`);
};
export const getEntityBlame = (owner: string, repo: string, ref: string, key: string, path?: string, limit?: number) => {
  const params = new URLSearchParams({ key });
  if (path) params.set('path', path);
  if (limit && limit > 0) params.set('limit', String(limit));
  return request<EntityBlameInfo>('GET', `/repos/${owner}/${repo}/entity-blame/${ref}?${params.toString()}`);
};
export const getDiff = (owner: string, repo: string, spec: string) =>
  request<{ files: DiffFile[] }>('GET', `/repos/${owner}/${repo}/diff/${spec}`);

// Pull Requests
export const createPR = (owner: string, repo: string, data: Record<string, unknown>) =>
  request<PullRequest>('POST', `/repos/${owner}/${repo}/pulls`, data);
export const listPRs = (owner: string, repo: string, state?: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (state) query.set('state', state);
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<PullRequest[]>('GET', `/repos/${owner}/${repo}/pulls${suffix ? '?' + suffix : ''}`);
};
export const getPR = (owner: string, repo: string, number: number) =>
  request<PullRequest>('GET', `/repos/${owner}/${repo}/pulls/${number}`);
export const updatePR = (owner: string, repo: string, number: number, data: { title?: string; body?: string }) =>
  request<PullRequest>('PATCH', `/repos/${owner}/${repo}/pulls/${number}`, data);
export const getPRDiff = (owner: string, repo: string, number: number) =>
  request<{ files: DiffFile[] }>('GET', `/repos/${owner}/${repo}/pulls/${number}/diff`);
export const getMergePreview = (owner: string, repo: string, number: number) =>
  request<MergePreviewResponse>('GET', `/repos/${owner}/${repo}/pulls/${number}/merge-preview`);
export const getMergeGate = (owner: string, repo: string, number: number) =>
  request<MergeGate>('GET', `/repos/${owner}/${repo}/pulls/${number}/merge-gate`);
export const mergePR = (owner: string, repo: string, number: number) =>
  request<{ merge_commit: string; status: string }>('POST', `/repos/${owner}/${repo}/pulls/${number}/merge`);
export const listPRComments = (owner: string, repo: string, number: number) =>
  request<PRComment[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/comments`);
export const createPRComment = (owner: string, repo: string, number: number, data: Record<string, unknown>) =>
  request<PRComment>('POST', `/repos/${owner}/${repo}/pulls/${number}/comments`, data);
export const deletePRComment = (owner: string, repo: string, number: number, commentId: number) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/pulls/${number}/comments/${commentId}`);
export const listPRReviews = (owner: string, repo: string, number: number) =>
  request<PRReview[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/reviews`);
export const createPRReview = (owner: string, repo: string, number: number, data: Record<string, unknown>) =>
  request<PRReview>('POST', `/repos/${owner}/${repo}/pulls/${number}/reviews`, data);
export const listPRChecks = (owner: string, repo: string, number: number) =>
  request<CheckRun[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/checks`);
export const upsertPRCheck = (owner: string, repo: string, number: number, data: Record<string, unknown>) =>
  request<CheckRun>('POST', `/repos/${owner}/${repo}/pulls/${number}/checks`, data);

// Branch protection
export const getBranchProtection = (owner: string, repo: string, branch: string) =>
  request<BranchProtectionRule>('GET', `/repos/${owner}/${repo}/branch-protection/${branch}`);
export const setBranchProtection = (owner: string, repo: string, branch: string, data: Record<string, unknown>) =>
  request<BranchProtectionRule>('PUT', `/repos/${owner}/${repo}/branch-protection/${branch}`, data);
export const deleteBranchProtection = (owner: string, repo: string, branch: string) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/branch-protection/${branch}`);

// Webhooks
export const createWebhook = (owner: string, repo: string, data: Record<string, unknown>) =>
  request<Webhook>('POST', `/repos/${owner}/${repo}/webhooks`, data);
export const listWebhooks = (owner: string, repo: string) =>
  request<Webhook[]>('GET', `/repos/${owner}/${repo}/webhooks`);
export const getWebhook = (owner: string, repo: string, id: number) =>
  request<Webhook>('GET', `/repos/${owner}/${repo}/webhooks/${id}`);
export const deleteWebhook = (owner: string, repo: string, id: number) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/webhooks/${id}`);
export const listWebhookDeliveries = (owner: string, repo: string, id: number) =>
  request<WebhookDelivery[]>('GET', `/repos/${owner}/${repo}/webhooks/${id}/deliveries`);
export const pingWebhook = (owner: string, repo: string, id: number) =>
  request<WebhookDelivery>('POST', `/repos/${owner}/${repo}/webhooks/${id}/ping`);
export const redeliverWebhookDelivery = (owner: string, repo: string, id: number, deliveryID: number) =>
  request<WebhookDelivery>('POST', `/repos/${owner}/${repo}/webhooks/${id}/deliveries/${deliveryID}/redeliver`);

// SSH Keys
export const listSSHKeys = () => request<SSHKey[]>('GET', '/user/ssh-keys');
export const createSSHKey = (name: string, publicKey: string) =>
  request<SSHKey>('POST', '/user/ssh-keys', { name, public_key: publicKey });
export const deleteSSHKey = (id: number) => request<void>('DELETE', `/user/ssh-keys/${id}`);

// Collaborators
export const listCollaborators = (owner: string, repo: string) =>
  request<Collaborator[]>('GET', `/repos/${owner}/${repo}/collaborators`);
export const addCollaborator = (owner: string, repo: string, username: string, role: string) =>
  request<Collaborator>('POST', `/repos/${owner}/${repo}/collaborators`, { username, role });
export const removeCollaborator = (owner: string, repo: string, username: string) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/collaborators/${username}`);

// Repo management
export const deleteRepo = (owner: string, repo: string) =>
  request<void>('DELETE', `/repos/${owner}/${repo}`);

// Organizations
export const createOrg = (name: string, displayName: string) =>
  request<Organization>('POST', '/orgs', { name, display_name: displayName });
export const getOrg = (org: string) => request<Organization>('GET', `/orgs/${org}`);
export const deleteOrg = (org: string) => request<void>('DELETE', `/orgs/${org}`);
export const listOrgMembers = (org: string) => request<Collaborator[]>('GET', `/orgs/${org}/members`);
export const addOrgMember = (org: string, username: string, role: string) =>
  request<void>('POST', `/orgs/${org}/members`, { username, role });
export const removeOrgMember = (org: string, username: string) =>
  request<void>('DELETE', `/orgs/${org}/members/${username}`);
export const listOrgRepos = (org: string) => request<Repository[]>('GET', `/orgs/${org}/repos`);
export const listUserOrgs = () => request<Organization[]>('GET', '/user/orgs');

// Code intelligence
export const searchSymbols = (owner: string, repo: string, ref: string, query?: string) => {
  const params = new URLSearchParams();
  if (query) params.set('q', query);
  const suffix = params.toString();
  return request<SymbolResult[]>('GET', `/repos/${owner}/${repo}/symbols/${ref}${suffix ? '?' + suffix : ''}`);
};
export const findReferences = (owner: string, repo: string, ref: string, name: string) =>
  request<ReferenceResult[]>('GET', `/repos/${owner}/${repo}/references/${ref}?name=${encodeURIComponent(name)}`);
export const getCallGraph = (owner: string, repo: string, ref: string, symbol: string, depth?: number, reverse?: boolean) => {
  const params = new URLSearchParams({ symbol });
  if (depth) params.set('depth', String(depth));
  if (reverse) params.set('reverse', 'true');
  return request<CallGraphResponse>('GET', `/repos/${owner}/${repo}/callgraph/${ref}?${params.toString()}`);
};

// Entity history
export const getEntityHistory = (owner: string, repo: string, ref: string, opts: { stableId?: string; name?: string; bodyHash?: string; limit?: number }) => {
  const params = new URLSearchParams();
  if (opts.stableId) params.set('stable_id', opts.stableId);
  if (opts.name) params.set('name', opts.name);
  if (opts.bodyHash) params.set('body_hash', opts.bodyHash);
  if (opts.limit && opts.limit > 0) params.set('limit', String(opts.limit));
  return request<EntityHistoryHit[]>('GET', `/repos/${owner}/${repo}/entity-history/${ref}?${params.toString()}`);
};

// Semver
export const getSemver = (owner: string, repo: string, spec: string) =>
  request<SemverRecommendation>('GET', `/repos/${owner}/${repo}/semver/${spec}`);

// Issues
export const createIssue = (owner: string, repo: string, data: Record<string, unknown>) =>
  request<Issue>('POST', `/repos/${owner}/${repo}/issues`, data);
export const listIssues = (owner: string, repo: string, state?: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (state) query.set('state', state);
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<Issue[]>('GET', `/repos/${owner}/${repo}/issues${suffix ? '?' + suffix : ''}`);
};
export const getIssue = (owner: string, repo: string, number: number) =>
  request<Issue>('GET', `/repos/${owner}/${repo}/issues/${number}`);
export const updateIssue = (owner: string, repo: string, number: number, data: Record<string, unknown>) =>
  request<Issue>('PATCH', `/repos/${owner}/${repo}/issues/${number}`, data);
export const createIssueComment = (owner: string, repo: string, number: number, data: Record<string, unknown>) =>
  request<IssueComment>('POST', `/repos/${owner}/${repo}/issues/${number}/comments`, data);
export const listIssueComments = (owner: string, repo: string, number: number) =>
  request<IssueComment[]>('GET', `/repos/${owner}/${repo}/issues/${number}/comments`);
export const deleteIssueComment = (owner: string, repo: string, number: number, commentId: number) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/issues/${number}/comments/${commentId}`);

export function streamRepoEvents(owner: string, repo: string, handlers: RepoEventStreamHandlers): RepoEventStreamSubscription {
  const controller = new AbortController();
  const path = `/repos/${owner}/${repo}/events`;
  const headers: Record<string, string> = { Accept: 'text/event-stream' };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const emitError = (error: Error) => {
    if (!controller.signal.aborted) {
      handlers.onError?.(error);
    }
  };

  const emitEvent = (eventName: string, dataLines: string[]) => {
    if (dataLines.length === 0) return;
    const raw = dataLines.join('\n');
    try {
      const parsed = JSON.parse(raw) as RepoStreamEvent;
      const type = eventName || parsed.type || 'message';
      handlers.onEvent({ ...parsed, type });
    } catch {
      emitError(new Error('failed to parse repository event stream payload'));
    }
  };

  (async () => {
    try {
      const resp = await fetch(`${BASE}${path}`, {
        method: 'GET',
        headers,
        signal: controller.signal,
      });

      if (resp.status === 401) {
        setToken(null);
        if (typeof window !== 'undefined' && !redirectingToLogin && !window.location.pathname.startsWith('/login')) {
          redirectingToLogin = true;
          window.location.assign(buildLoginRedirectPath());
        }
        throw new Error('authentication required');
      }

      if (!resp.ok) {
        let message = resp.statusText;
        try {
          const payload = await resp.json() as { error?: string };
          if (payload?.error) message = payload.error;
        } catch {
          // Keep status text fallback.
        }
        throw new Error(message || 'failed to open event stream');
      }

      if (!resp.body) {
        throw new Error('event stream body missing');
      }

      handlers.onOpen?.();

      const decoder = new TextDecoder();
      const reader = resp.body.getReader();
      let buffer = '';
      let eventName = '';
      let dataLines: string[] = [];

      while (!controller.signal.aborted) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        while (true) {
          const newline = buffer.indexOf('\n');
          if (newline < 0) break;

          let line = buffer.slice(0, newline);
          buffer = buffer.slice(newline + 1);
          if (line.endsWith('\r')) line = line.slice(0, -1);

          if (line === '') {
            emitEvent(eventName, dataLines);
            eventName = '';
            dataLines = [];
            continue;
          }
          if (line.startsWith(':')) continue;
          if (line.startsWith('event:')) {
            eventName = line.slice('event:'.length).trim();
            continue;
          }
          if (line.startsWith('data:')) {
            dataLines.push(line.slice('data:'.length).trimStart());
          }
        }
      }
    } catch (err: any) {
      if (controller.signal.aborted) return;
      emitError(err instanceof Error ? err : new Error(err?.message || 'event stream failed'));
    }
  })();

  return {
    close: () => controller.abort(),
  };
}
