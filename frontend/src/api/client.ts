const BASE = '/api/v1';

let token: string | null = localStorage.getItem('gothub_token');

export function setToken(t: string | null) {
  token = t;
  if (t) localStorage.setItem('gothub_token', t);
  else localStorage.removeItem('gothub_token');
}

export function getToken() { return token; }

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const resp = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || resp.statusText);
  }

  if (resp.status === 204) return undefined as T;
  return resp.json();
}

// Auth
export const register = (username: string, email: string, password: string) =>
  request<{ token: string; user: any }>('POST', '/auth/register', { username, email, password });
export const login = (username: string, password: string) =>
  request<{ token: string; user: any }>('POST', '/auth/login', { username, password });
export const getUser = () => request<any>('GET', '/user');
export const listNotifications = (unread?: boolean, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (unread) query.set('unread', 'true');
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<any[]>('GET', `/notifications${suffix ? '?' + suffix : ''}`);
};
export const getUnreadNotificationsCount = () => request<{ count: number }>('GET', '/notifications/unread-count');
export const markNotificationRead = (id: number) => request<void>('POST', `/notifications/${id}/read`);
export const markAllNotificationsRead = () => request<void>('POST', '/notifications/read-all');

// Repos
export const createRepo = (name: string, description: string, isPrivate: boolean) =>
  request<any>('POST', '/repos', { name, description, private: isPrivate });
export const getRepo = (owner: string, repo: string) =>
  request<any>('GET', `/repos/${owner}/${repo}`);
export const listUserRepos = () => request<any[]>('GET', '/user/repos');
export const listUserStarredRepos = () => request<any[]>('GET', '/user/starred');
export const getRepoStars = (owner: string, repo: string) =>
  request<{ count: number; starred: boolean }>('GET', `/repos/${owner}/${repo}/stars`);
export const starRepo = (owner: string, repo: string) =>
  request<{ count: number; starred: boolean }>('PUT', `/repos/${owner}/${repo}/star`);
export const unstarRepo = (owner: string, repo: string) =>
  request<{ count: number; starred: boolean }>('DELETE', `/repos/${owner}/${repo}/star`);
export const listRepoStargazers = (owner: string, repo: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<any[]>('GET', `/repos/${owner}/${repo}/stargazers${suffix ? '?' + suffix : ''}`);
};

// Browsing
export const listTree = (owner: string, repo: string, ref: string, path?: string) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/tree/${ref}${path ? '/' + path : ''}`);
export const listBranches = (owner: string, repo: string) =>
  request<string[]>('GET', `/repos/${owner}/${repo}/branches`);
export const getBlob = (owner: string, repo: string, ref: string, path: string) =>
  request<any>('GET', `/repos/${owner}/${repo}/blob/${ref}/${path}`);
export const listCommits = (owner: string, repo: string, ref: string) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/commits/${ref}`);
export const getCommit = (owner: string, repo: string, hash: string) =>
  request<any>('GET', `/repos/${owner}/${repo}/commit/${hash}`);

// Entities & Diff
export const listEntities = (owner: string, repo: string, ref: string, path: string) =>
  request<any>('GET', `/repos/${owner}/${repo}/entities/${ref}/${path}`);
export const getDiff = (owner: string, repo: string, spec: string) =>
  request<any>('GET', `/repos/${owner}/${repo}/diff/${spec}`);

// Pull Requests
export const createPR = (owner: string, repo: string, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/pulls`, data);
export const listPRs = (owner: string, repo: string, state?: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (state) query.set('state', state);
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<any[]>('GET', `/repos/${owner}/${repo}/pulls${suffix ? '?' + suffix : ''}`);
};
export const getPR = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}`);
export const getPRDiff = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}/diff`);
export const getMergePreview = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}/merge-preview`);
export const getMergeGate = (owner: string, repo: string, number: number) =>
  request<{ allowed: boolean; reasons?: string[] }>('GET', `/repos/${owner}/${repo}/pulls/${number}/merge-gate`);
export const mergePR = (owner: string, repo: string, number: number) =>
  request<any>('POST', `/repos/${owner}/${repo}/pulls/${number}/merge`);
export const listPRComments = (owner: string, repo: string, number: number) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/comments`);
export const createPRComment = (owner: string, repo: string, number: number, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/pulls/${number}/comments`, data);
export const listPRReviews = (owner: string, repo: string, number: number) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/reviews`);
export const createPRReview = (owner: string, repo: string, number: number, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/pulls/${number}/reviews`, data);
export const listPRChecks = (owner: string, repo: string, number: number) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/pulls/${number}/checks`);
export const upsertPRCheck = (owner: string, repo: string, number: number, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/pulls/${number}/checks`, data);

// Branch protection
export const getBranchProtection = (owner: string, repo: string, branch: string) =>
  request<any>('GET', `/repos/${owner}/${repo}/branch-protection/${branch}`);
export const setBranchProtection = (owner: string, repo: string, branch: string, data: any) =>
  request<any>('PUT', `/repos/${owner}/${repo}/branch-protection/${branch}`, data);
export const deleteBranchProtection = (owner: string, repo: string, branch: string) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/branch-protection/${branch}`);

// Webhooks
export const createWebhook = (owner: string, repo: string, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/webhooks`, data);
export const listWebhooks = (owner: string, repo: string) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/webhooks`);
export const getWebhook = (owner: string, repo: string, id: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/webhooks/${id}`);
export const deleteWebhook = (owner: string, repo: string, id: number) =>
  request<void>('DELETE', `/repos/${owner}/${repo}/webhooks/${id}`);
export const listWebhookDeliveries = (owner: string, repo: string, id: number) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/webhooks/${id}/deliveries`);
export const pingWebhook = (owner: string, repo: string, id: number) =>
  request<any>('POST', `/repos/${owner}/${repo}/webhooks/${id}/ping`);
export const redeliverWebhookDelivery = (owner: string, repo: string, id: number, deliveryID: number) =>
  request<any>('POST', `/repos/${owner}/${repo}/webhooks/${id}/deliveries/${deliveryID}/redeliver`);

// Issues
export const createIssue = (owner: string, repo: string, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/issues`, data);
export const listIssues = (owner: string, repo: string, state?: string, page?: number, perPage?: number) => {
  const query = new URLSearchParams();
  if (state) query.set('state', state);
  if (page && page > 0) query.set('page', String(page));
  if (perPage && perPage > 0) query.set('per_page', String(perPage));
  const suffix = query.toString();
  return request<any[]>('GET', `/repos/${owner}/${repo}/issues${suffix ? '?' + suffix : ''}`);
};
export const getIssue = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/issues/${number}`);
export const updateIssue = (owner: string, repo: string, number: number, data: any) =>
  request<any>('PATCH', `/repos/${owner}/${repo}/issues/${number}`, data);
export const createIssueComment = (owner: string, repo: string, number: number, data: any) =>
  request<any>('POST', `/repos/${owner}/${repo}/issues/${number}/comments`, data);
export const listIssueComments = (owner: string, repo: string, number: number) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/issues/${number}/comments`);
