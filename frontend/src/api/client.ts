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

// Repos
export const createRepo = (name: string, description: string, isPrivate: boolean) =>
  request<any>('POST', '/repos', { name, description, private: isPrivate });
export const getRepo = (owner: string, repo: string) =>
  request<any>('GET', `/repos/${owner}/${repo}`);
export const listUserRepos = () => request<any[]>('GET', '/user/repos');

// Browsing
export const listTree = (owner: string, repo: string, ref: string, path?: string) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/tree/${ref}${path ? '/' + path : ''}`);
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
export const listPRs = (owner: string, repo: string, state?: string) =>
  request<any[]>('GET', `/repos/${owner}/${repo}/pulls${state ? '?state=' + state : ''}`);
export const getPR = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}`);
export const getPRDiff = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}/diff`);
export const getMergePreview = (owner: string, repo: string, number: number) =>
  request<any>('GET', `/repos/${owner}/${repo}/pulls/${number}/merge-preview`);
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
