import { useState, useEffect } from 'preact/hooks';
import {
  getToken, getRepo, deleteRepo, listBranches,
  listCollaborators, addCollaborator, removeCollaborator,
  listWebhooks, createWebhook, deleteWebhook, listWebhookDeliveries, pingWebhook, redeliverWebhookDelivery,
  getBranchProtection, setBranchProtection, deleteBranchProtection,
  listRepoRunnerTokens, createRepoRunnerToken, deleteRepoRunnerToken,
} from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  path?: string;
}

type Tab = 'general' | 'collaborators' | 'webhooks' | 'runners' | 'branch-protection';

const TABS: { key: Tab; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'collaborators', label: 'Collaborators' },
  { key: 'webhooks', label: 'Webhooks' },
  { key: 'runners', label: 'Runners' },
  { key: 'branch-protection', label: 'Branch Protection' },
];

// ---------------------------------------------------------------------------
// Style constants
// ---------------------------------------------------------------------------

const colors = {
  bg: '#0d1117',
  text: '#c9d1d9',
  heading: '#f0f6fc',
  muted: '#8b949e',
  link: '#58a6ff',
  green: '#238636',
  red: '#f85149',
  border: '#30363d',
  surface: '#161b22',
  active: '#1f6feb',
} as const;

const inputStyle: Record<string, string> = {
  background: colors.bg,
  border: `1px solid ${colors.border}`,
  borderRadius: '6px',
  padding: '10px 12px',
  color: colors.text,
  fontSize: '14px',
};

const tabBaseStyle: Record<string, string> = {
  color: colors.text,
  padding: '8px 16px',
  borderRadius: '6px',
  fontSize: '14px',
  border: `1px solid ${colors.border}`,
  cursor: 'pointer',
  background: colors.surface,
};

const tabActiveStyle: Record<string, string> = {
  ...tabBaseStyle,
  background: colors.active,
};

const btnPrimary: Record<string, string> = {
  background: colors.green,
  color: '#fff',
  border: 'none',
  padding: '8px 16px',
  borderRadius: '6px',
  cursor: 'pointer',
  fontSize: '14px',
  fontWeight: 'bold',
};

const btnDanger: Record<string, string> = {
  background: colors.red,
  color: '#fff',
  border: 'none',
  padding: '8px 16px',
  borderRadius: '6px',
  cursor: 'pointer',
  fontSize: '14px',
  fontWeight: 'bold',
};

const btnSecondary: Record<string, string> = {
  background: colors.surface,
  color: colors.text,
  border: `1px solid ${colors.border}`,
  padding: '8px 16px',
  borderRadius: '6px',
  cursor: 'pointer',
  fontSize: '14px',
};

const sectionBox: Record<string, string> = {
  border: `1px solid ${colors.border}`,
  borderRadius: '6px',
  padding: '20px',
  marginBottom: '20px',
  background: colors.surface,
};

const labelStyle: Record<string, string> = {
  color: colors.heading,
  fontSize: '14px',
  fontWeight: 'bold',
  marginBottom: '6px',
  display: 'block',
};

// ---------------------------------------------------------------------------
// Top-level component
// ---------------------------------------------------------------------------

export function RepoSettingsView({ owner, repo }: Props) {
  const [activeTab, setActiveTab] = useState<Tab>('general');
  const [repoInfo, setRepoInfo] = useState<any>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo) return;
    getRepo(owner, repo).then(setRepoInfo).catch(e => setError(e.message));
  }, [owner, repo]);

  if (!owner || !repo) {
    return <div style={{ color: colors.red, padding: '20px' }}>Missing repository context.</div>;
  }

  if (!getToken()) {
    return <div style={{ color: colors.red, padding: '20px' }}>You must be logged in to access repository settings.</div>;
  }
  if (error) return <div style={{ color: colors.red, padding: '20px' }}>{error}</div>;
  if (!repoInfo) return <div style={{ color: colors.muted, padding: '20px' }}>Loading...</div>;

  return (
    <div>
      <h1 style={{ fontSize: '20px', color: colors.heading, marginBottom: '4px' }}>
        <a href={`/${owner}`} style={{ color: colors.link, textDecoration: 'none' }}>{owner}</a>
        <span style={{ color: colors.muted }}> / </span>
        <a href={`/${owner}/${repo}`} style={{ color: colors.link, fontWeight: 'bold', textDecoration: 'none' }}>{repo}</a>
        <span style={{ color: colors.muted, fontWeight: 'normal' }}> - Settings</span>
      </h1>

      {/* Tab bar */}
      <div style={{ display: 'flex', gap: '8px', marginTop: '20px', marginBottom: '24px', flexWrap: 'wrap' }}>
        {TABS.map(t => (
          <button
            key={t.key}
            onClick={() => setActiveTab(t.key)}
            style={activeTab === t.key ? tabActiveStyle : tabBaseStyle}
          >
            {t.label}
          </button>
        ))}
      </div>

      {activeTab === 'general' && <GeneralTab owner={owner} repo={repo} repoInfo={repoInfo} />}
      {activeTab === 'collaborators' && <CollaboratorsTab owner={owner} repo={repo} />}
      {activeTab === 'webhooks' && <WebhooksTab owner={owner} repo={repo} />}
      {activeTab === 'runners' && <RunnersTab owner={owner} repo={repo} />}
      {activeTab === 'branch-protection' && <BranchProtectionTab owner={owner} repo={repo} />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 1: General
// ---------------------------------------------------------------------------

function GeneralTab({ owner, repo, repoInfo }: { owner: string; repo: string; repoInfo: any }) {
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState('');

  const handleDelete = async () => {
    const confirmed = window.confirm(
      `Are you sure you want to delete "${owner}/${repo}"? This action cannot be undone.`
    );
    if (!confirmed) return;

    setDeleting(true);
    setError('');
    try {
      await deleteRepo(owner, repo);
      location.href = '/';
    } catch (e: any) {
      setError(e.message || 'Failed to delete repository');
      setDeleting(false);
    }
  };

  return (
    <div>
      {/* Repo name (read-only) */}
      <div style={sectionBox}>
        <label style={labelStyle}>Repository name</label>
        <input
          type="text"
          value={`${owner}/${repo}`}
          readOnly
          style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any, opacity: '0.7', cursor: 'not-allowed' }}
        />
        {repoInfo.description && (
          <div style={{ marginTop: '12px' }}>
            <label style={labelStyle}>Description</label>
            <div style={{ color: colors.text, fontSize: '14px' }}>{repoInfo.description}</div>
          </div>
        )}
        {repoInfo.parent_owner && repoInfo.parent_name && (
          <div style={{ marginTop: '12px', color: colors.muted, fontSize: '13px' }}>
            forked from{' '}
            <a href={`/${repoInfo.parent_owner}/${repoInfo.parent_name}`} style={{ color: colors.link, textDecoration: 'none' }}>
              {repoInfo.parent_owner}/{repoInfo.parent_name}
            </a>
          </div>
        )}
      </div>

      {/* Danger zone */}
      <div style={{
        border: `2px solid ${colors.red}`,
        borderRadius: '6px',
        padding: '20px',
        background: colors.surface,
      }}>
        <h3 style={{ color: colors.red, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Danger Zone</h3>

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '12px' }}>
          <div>
            <div style={{ color: colors.heading, fontSize: '14px', fontWeight: 'bold' }}>Delete this repository</div>
            <div style={{ color: colors.muted, fontSize: '13px' }}>Once you delete a repository, there is no going back.</div>
          </div>
          <button onClick={handleDelete} disabled={deleting} style={{ ...btnDanger, opacity: deleting ? '0.6' : '1' }}>
            {deleting ? 'Deleting...' : 'Delete this repository'}
          </button>
        </div>

        {error && <div style={{ color: colors.red, marginTop: '12px', fontSize: '13px' }}>{error}</div>}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 2: Collaborators
// ---------------------------------------------------------------------------

function CollaboratorsTab({ owner, repo }: { owner: string; repo: string }) {
  const [collaborators, setCollaborators] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const [newUsername, setNewUsername] = useState('');
  const [newRole, setNewRole] = useState('read');
  const [adding, setAdding] = useState(false);

  const fetchCollaborators = async () => {
    setLoading(true);
    try {
      const list = await listCollaborators(owner, repo);
      setCollaborators(list || []);
    } catch (e: any) {
      setError(e.message);
    }
    setLoading(false);
  };

  useEffect(() => { fetchCollaborators(); }, [owner, repo]);

  const handleAdd = async (e: Event) => {
    e.preventDefault();
    if (!newUsername.trim()) return;
    setAdding(true);
    setError('');
    try {
      await addCollaborator(owner, repo, newUsername.trim(), newRole);
      setNewUsername('');
      setNewRole('read');
      await fetchCollaborators();
    } catch (e: any) {
      setError(e.message || 'Failed to add collaborator');
    }
    setAdding(false);
  };

  const handleRemove = async (username: string) => {
    const confirmed = window.confirm(`Remove collaborator "${username}"?`);
    if (!confirmed) return;
    setError('');
    try {
      await removeCollaborator(owner, repo, username);
      await fetchCollaborators();
    } catch (e: any) {
      setError(e.message || 'Failed to remove collaborator');
    }
  };

  const roleBadge = (role: string) => {
    const bg = role === 'admin' ? '#8957e5' : role === 'write' ? colors.green : colors.border;
    return (
      <span style={{
        background: bg,
        color: '#fff',
        padding: '2px 8px',
        borderRadius: '12px',
        fontSize: '12px',
        fontWeight: 'bold',
        marginLeft: '8px',
      }}>
        {role}
      </span>
    );
  };

  return (
    <div>
      {error && <div style={{ color: colors.red, marginBottom: '12px', fontSize: '13px' }}>{error}</div>}

      {/* Add collaborator form */}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Add collaborator</h3>
        <form onSubmit={handleAdd} style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <div style={{ flex: '1', minWidth: '200px' }}>
            <label style={labelStyle}>Username</label>
            <input
              type="text"
              value={newUsername}
              onInput={(e: any) => setNewUsername(e.target.value)}
              placeholder="username"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>
          <div style={{ minWidth: '120px' }}>
            <label style={labelStyle}>Role</label>
            <select
              value={newRole}
              onChange={(e: any) => setNewRole(e.target.value)}
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            >
              <option value="read">read</option>
              <option value="write">write</option>
              <option value="admin">admin</option>
            </select>
          </div>
          <button type="submit" disabled={adding || !newUsername.trim()} style={{ ...btnPrimary, opacity: adding || !newUsername.trim() ? '0.6' : '1' }}>
            {adding ? 'Adding...' : 'Add'}
          </button>
        </form>
      </div>

      {/* Collaborator list */}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Collaborators</h3>
        {loading ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>Loading...</div>
        ) : collaborators.length === 0 ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>No collaborators yet.</div>
        ) : (
          collaborators.map((c, idx) => (
            <div
              key={c.username || idx}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '10px 0',
                borderTop: idx === 0 ? 'none' : `1px solid ${colors.border}`,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center' }}>
                <span style={{ color: colors.text, fontSize: '14px', fontWeight: 'bold' }}>{c.username}</span>
                {roleBadge(c.role)}
              </div>
              <button
                onClick={() => handleRemove(c.username)}
                style={{ ...btnDanger, padding: '4px 10px', fontSize: '12px' }}
              >
                Remove
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 3: Webhooks
// ---------------------------------------------------------------------------

function WebhooksTab({ owner, repo }: { owner: string; repo: string }) {
  const [webhooks, setWebhooks] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Form state
  const [newUrl, setNewUrl] = useState('');
  const [newSecret, setNewSecret] = useState('');
  const [newEvents, setNewEvents] = useState('');
  const [creating, setCreating] = useState(false);

  // Expanded webhook deliveries
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [deliveries, setDeliveries] = useState<any[]>([]);
  const [loadingDeliveries, setLoadingDeliveries] = useState(false);

  const fetchWebhooks = async () => {
    setLoading(true);
    try {
      const list = await listWebhooks(owner, repo);
      setWebhooks(list || []);
    } catch (e: any) {
      setError(e.message);
    }
    setLoading(false);
  };

  useEffect(() => { fetchWebhooks(); }, [owner, repo]);

  const handleCreate = async (e: Event) => {
    e.preventDefault();
    if (!newUrl.trim()) return;
    setCreating(true);
    setError('');
    try {
      const events = newEvents
        .split(',')
        .map(s => s.trim())
        .filter(Boolean);
      const payload: any = { url: newUrl.trim() };
      if (newSecret.trim()) payload.secret = newSecret.trim();
      if (events.length > 0) payload.events = events;
      await createWebhook(owner, repo, payload);
      setNewUrl('');
      setNewSecret('');
      setNewEvents('');
      await fetchWebhooks();
    } catch (e: any) {
      setError(e.message || 'Failed to create webhook');
    }
    setCreating(false);
  };

  const handleDelete = async (id: number) => {
    const confirmed = window.confirm(`Delete webhook #${id}?`);
    if (!confirmed) return;
    setError('');
    try {
      await deleteWebhook(owner, repo, id);
      if (expandedId === id) {
        setExpandedId(null);
        setDeliveries([]);
      }
      await fetchWebhooks();
    } catch (e: any) {
      setError(e.message || 'Failed to delete webhook');
    }
  };

  const handlePing = async (id: number) => {
    setError('');
    try {
      await pingWebhook(owner, repo, id);
    } catch (e: any) {
      setError(e.message || 'Ping failed');
    }
  };

  const toggleDeliveries = async (id: number) => {
    if (expandedId === id) {
      setExpandedId(null);
      setDeliveries([]);
      return;
    }
    setExpandedId(id);
    setLoadingDeliveries(true);
    try {
      const list = await listWebhookDeliveries(owner, repo, id);
      setDeliveries(list || []);
    } catch (e: any) {
      setError(e.message || 'Failed to load deliveries');
      setDeliveries([]);
    }
    setLoadingDeliveries(false);
  };

  const handleRedeliver = async (webhookId: number, deliveryId: number) => {
    setError('');
    try {
      await redeliverWebhookDelivery(owner, repo, webhookId, deliveryId);
      // Refresh deliveries
      const list = await listWebhookDeliveries(owner, repo, webhookId);
      setDeliveries(list || []);
    } catch (e: any) {
      setError(e.message || 'Redelivery failed');
    }
  };

  return (
    <div>
      {error && <div style={{ color: colors.red, marginBottom: '12px', fontSize: '13px' }}>{error}</div>}

      {/* Create webhook form */}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Add webhook</h3>
        <form onSubmit={handleCreate}>
          <div style={{ marginBottom: '12px' }}>
            <label style={labelStyle}>Payload URL</label>
            <input
              type="text"
              value={newUrl}
              onInput={(e: any) => setNewUrl(e.target.value)}
              placeholder="https://example.com/webhook"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>
          <div style={{ marginBottom: '12px' }}>
            <label style={labelStyle}>Secret (optional)</label>
            <input
              type="text"
              value={newSecret}
              onInput={(e: any) => setNewSecret(e.target.value)}
              placeholder="webhook secret"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>
          <div style={{ marginBottom: '12px' }}>
            <label style={labelStyle}>Events (comma-separated)</label>
            <input
              type="text"
              value={newEvents}
              onInput={(e: any) => setNewEvents(e.target.value)}
              placeholder="push, pull_request, issues"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>
          <button type="submit" disabled={creating || !newUrl.trim()} style={{ ...btnPrimary, opacity: creating || !newUrl.trim() ? '0.6' : '1' }}>
            {creating ? 'Creating...' : 'Create webhook'}
          </button>
        </form>
      </div>

      {/* Webhook list */}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Webhooks</h3>
        {loading ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>Loading...</div>
        ) : webhooks.length === 0 ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>No webhooks configured.</div>
        ) : (
          webhooks.map((wh, idx) => (
            <div key={wh.id || idx}>
              <div style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                padding: '12px 0',
                borderTop: idx === 0 ? 'none' : `1px solid ${colors.border}`,
                flexWrap: 'wrap',
                gap: '8px',
              }}>
                <div
                  onClick={() => toggleDeliveries(wh.id)}
                  style={{ cursor: 'pointer', flex: '1', minWidth: '200px' }}
                >
                  <div style={{ color: colors.link, fontSize: '14px', fontWeight: 'bold' }}>{wh.url}</div>
                  <div style={{ color: colors.muted, fontSize: '12px' }}>ID: {wh.id}</div>
                </div>
                <div style={{ display: 'flex', gap: '6px' }}>
                  <button onClick={() => handlePing(wh.id)} style={{ ...btnSecondary, padding: '4px 10px', fontSize: '12px' }}>
                    Ping
                  </button>
                  <button onClick={() => handleDelete(wh.id)} style={{ ...btnDanger, padding: '4px 10px', fontSize: '12px' }}>
                    Delete
                  </button>
                </div>
              </div>

              {/* Deliveries (expandable) */}
              {expandedId === wh.id && (
                <div style={{
                  marginLeft: '16px',
                  marginBottom: '12px',
                  padding: '12px',
                  background: colors.bg,
                  border: `1px solid ${colors.border}`,
                  borderRadius: '6px',
                }}>
                  <h4 style={{ color: colors.heading, fontSize: '13px', marginTop: '0', marginBottom: '10px' }}>Recent Deliveries</h4>
                  {loadingDeliveries ? (
                    <div style={{ color: colors.muted, fontSize: '13px' }}>Loading deliveries...</div>
                  ) : deliveries.length === 0 ? (
                    <div style={{ color: colors.muted, fontSize: '13px' }}>No deliveries yet.</div>
                  ) : (
                    deliveries.map((d, dIdx) => (
                      <div
                        key={d.id || dIdx}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          padding: '8px 0',
                          borderTop: dIdx === 0 ? 'none' : `1px solid ${colors.border}`,
                          flexWrap: 'wrap',
                          gap: '6px',
                        }}
                      >
                        <div>
                          <span style={{ color: colors.text, fontSize: '13px', fontFamily: 'monospace' }}>
                            {d.id}
                          </span>
                          <span style={{
                            marginLeft: '8px',
                            color: d.status === 'success' || d.response_code === 200 ? '#3fb950' : colors.red,
                            fontSize: '12px',
                            fontWeight: 'bold',
                          }}>
                            {d.status || (d.response_code != null ? `HTTP ${d.response_code}` : 'unknown')}
                          </span>
                          {d.created_at && (
                            <span style={{ color: colors.muted, fontSize: '12px', marginLeft: '8px' }}>
                              {new Date(d.created_at).toLocaleString()}
                            </span>
                          )}
                        </div>
                        <button
                          onClick={() => handleRedeliver(wh.id, d.id)}
                          style={{ ...btnSecondary, padding: '2px 8px', fontSize: '11px' }}
                        >
                          Redeliver
                        </button>
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 4: Runners
// ---------------------------------------------------------------------------

function RunnersTab({ owner, repo }: { owner: string; repo: string }) {
  const [tokens, setTokens] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [newName, setNewName] = useState('');
  const [expiresHours, setExpiresHours] = useState(24);
  const [creating, setCreating] = useState(false);
  const [lastIssuedToken, setLastIssuedToken] = useState('');

  const loadTokens = async () => {
    setLoading(true);
    setError('');
    try {
      const list = await listRepoRunnerTokens(owner, repo);
      setTokens(Array.isArray(list) ? list : []);
    } catch (e: any) {
      setError(e?.message || 'Failed to load runner tokens');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadTokens(); }, [owner, repo]);

  const handleCreate = async (e: Event) => {
    e.preventDefault();
    if (!newName.trim()) return;
    setCreating(true);
    setError('');
    setLastIssuedToken('');
    try {
      const payload: any = { name: newName.trim() };
      if (expiresHours > 0) payload.expires_in_hours = expiresHours;
      const created = await createRepoRunnerToken(owner, repo, payload);
      setLastIssuedToken(created.token || '');
      setNewName('');
      await loadTokens();
    } catch (e: any) {
      setError(e?.message || 'Failed to create runner token');
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (id: number) => {
    const confirmed = window.confirm('Revoke this runner token?');
    if (!confirmed) return;
    setError('');
    try {
      await deleteRepoRunnerToken(owner, repo, id);
      await loadTokens();
    } catch (e: any) {
      setError(e?.message || 'Failed to revoke runner token');
    }
  };

  return (
    <div>
      {error && <div style={{ color: colors.red, marginBottom: '12px', fontSize: '13px' }}>{error}</div>}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '10px' }}>Runner Tokens</h3>
        <div style={{ color: colors.muted, fontSize: '13px', marginBottom: '14px' }}>
          Use runner tokens for BYO CI runners to update PR check runs without user credentials.
        </div>
        {lastIssuedToken && (
          <div style={{ marginBottom: '12px', padding: '10px', background: colors.bg, border: `1px solid ${colors.border}`, borderRadius: '6px' }}>
            <div style={{ color: colors.heading, fontSize: '12px', marginBottom: '6px' }}>New token (shown once)</div>
            <code style={{ color: '#3fb950', fontSize: '12px', wordBreak: 'break-all' }}>{lastIssuedToken}</code>
          </div>
        )}
        <form onSubmit={handleCreate} style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', marginBottom: '16px' }}>
          <input
            type="text"
            value={newName}
            onInput={(e: any) => setNewName(e.target.value)}
            placeholder="runner name"
            style={{ ...inputStyle, minWidth: '200px', flex: '1' }}
          />
          <input
            type="number"
            min="0"
            max="8760"
            value={expiresHours}
            onInput={(e: any) => setExpiresHours(parseInt(e.target.value, 10) || 0)}
            style={{ ...inputStyle, width: '120px' }}
            title="Expires in hours (0 = never)"
          />
          <button type="submit" disabled={creating || !newName.trim()} style={{ ...btnPrimary, opacity: creating || !newName.trim() ? '0.6' : '1' }}>
            {creating ? 'Creating...' : 'Create token'}
          </button>
        </form>

        {loading ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>Loading tokens...</div>
        ) : tokens.length === 0 ? (
          <div style={{ color: colors.muted, fontSize: '14px' }}>No runner tokens yet.</div>
        ) : (
          tokens.map((tok, idx) => (
            <div
              key={tok.id || idx}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: '12px',
                borderTop: idx === 0 ? 'none' : `1px solid ${colors.border}`,
                padding: '10px 0',
                flexWrap: 'wrap',
              }}
            >
              <div>
                <div style={{ color: colors.heading, fontSize: '14px', fontWeight: 'bold' }}>{tok.name}</div>
                <div style={{ color: colors.muted, fontSize: '12px' }}>
                  {tok.token_prefix} • created {tok.created_at ? new Date(tok.created_at).toLocaleString() : 'n/a'}
                  {tok.last_used_at ? ` • last used ${new Date(tok.last_used_at).toLocaleString()}` : ''}
                  {tok.expires_at ? ` • expires ${new Date(tok.expires_at).toLocaleString()}` : ' • no expiry'}
                  {tok.revoked_at ? ' • revoked' : ''}
                </div>
              </div>
              <button
                onClick={() => handleRevoke(tok.id)}
                disabled={!!tok.revoked_at}
                style={{ ...btnDanger, opacity: tok.revoked_at ? '0.5' : '1', padding: '6px 10px', fontSize: '12px' }}
              >
                {tok.revoked_at ? 'Revoked' : 'Revoke'}
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Tab 5: Branch Protection
// ---------------------------------------------------------------------------

function BranchProtectionTab({ owner, repo }: { owner: string; repo: string }) {
  const [branches, setBranches] = useState<string[]>([]);
  const [selectedBranch, setSelectedBranch] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  // Protection rule fields
  const [requiredReviews, setRequiredReviews] = useState(0);
  const [requiredChecks, setRequiredChecks] = useState('');
  const [requireEntityOwnerApproval, setRequireEntityOwnerApproval] = useState(false);
  const [protectionLoaded, setProtectionLoaded] = useState(false);

  useEffect(() => {
    listBranches(owner, repo).then(b => setBranches(b || [])).catch((e: any) => setError(e.message || 'failed to list branches'));
  }, [owner, repo]);

  const loadProtection = async (branch: string) => {
    if (!branch) return;
    setLoading(true);
    setError('');
    setProtectionLoaded(false);
    try {
      const rule = await getBranchProtection(owner, repo, branch);
      setRequiredReviews(rule.required_reviews || 0);
      setRequiredChecks(
        Array.isArray(rule.required_checks) ? rule.required_checks.join(', ') : ''
      );
      setRequireEntityOwnerApproval(!!rule.require_entity_owner_approval);
      setProtectionLoaded(true);
    } catch (e: any) {
      // 404 means no protection rule exists yet -- start fresh
      setRequiredReviews(0);
      setRequiredChecks('');
      setRequireEntityOwnerApproval(false);
      setProtectionLoaded(true);
    }
    setLoading(false);
  };

  const handleBranchChange = (branch: string) => {
    setSelectedBranch(branch);
    if (branch) loadProtection(branch);
  };

  const handleSave = async () => {
    if (!selectedBranch) return;
    setSaving(true);
    setError('');
    try {
      const checks = requiredChecks
        .split(',')
        .map(s => s.trim())
        .filter(Boolean);
      await setBranchProtection(owner, repo, selectedBranch, {
        required_reviews: requiredReviews,
        required_checks: checks,
        require_entity_owner_approval: requireEntityOwnerApproval,
      });
    } catch (e: any) {
      setError(e.message || 'Failed to save branch protection');
    }
    setSaving(false);
  };

  const handleDeleteProtection = async () => {
    if (!selectedBranch) return;
    const confirmed = window.confirm(
      `Delete branch protection for "${selectedBranch}"?`
    );
    if (!confirmed) return;
    setError('');
    try {
      await deleteBranchProtection(owner, repo, selectedBranch);
      setRequiredReviews(0);
      setRequiredChecks('');
      setRequireEntityOwnerApproval(false);
    } catch (e: any) {
      setError(e.message || 'Failed to delete branch protection');
    }
  };

  return (
    <div>
      {error && <div style={{ color: colors.red, marginBottom: '12px', fontSize: '13px' }}>{error}</div>}

      {/* Branch selector */}
      <div style={sectionBox}>
        <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>Select branch</h3>
        {branches.length > 0 ? (
          <select
            value={selectedBranch}
            onChange={(e: any) => handleBranchChange(e.target.value)}
            style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
          >
            <option value="">-- select a branch --</option>
            {branches.map(b => (
              <option key={b} value={b}>{b}</option>
            ))}
          </select>
        ) : (
          <div>
            <label style={labelStyle}>Branch name</label>
            <input
              type="text"
              value={selectedBranch}
              onInput={(e: any) => setSelectedBranch(e.target.value)}
              onBlur={() => { if (selectedBranch) loadProtection(selectedBranch); }}
              placeholder="main"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>
        )}
      </div>

      {/* Protection rule form */}
      {loading && <div style={{ color: colors.muted, fontSize: '14px', marginBottom: '16px' }}>Loading protection rules...</div>}

      {protectionLoaded && selectedBranch && !loading && (
        <div style={sectionBox}>
          <h3 style={{ color: colors.heading, fontSize: '16px', marginTop: '0', marginBottom: '16px' }}>
            Protection rules for <code style={{ background: colors.bg, padding: '2px 6px', borderRadius: '4px' }}>{selectedBranch}</code>
          </h3>

          <div style={{ marginBottom: '16px' }}>
            <label style={labelStyle}>Required reviews</label>
            <input
              type="number"
              min="0"
              value={requiredReviews}
              onInput={(e: any) => setRequiredReviews(parseInt(e.target.value, 10) || 0)}
              style={{ ...inputStyle, width: '100px' }}
            />
          </div>

          <div style={{ marginBottom: '16px' }}>
            <label style={labelStyle}>Required checks (comma-separated)</label>
            <input
              type="text"
              value={requiredChecks}
              onInput={(e: any) => setRequiredChecks(e.target.value)}
              placeholder="ci/build, ci/test"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
            />
          </div>

          <div style={{ marginBottom: '20px' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={requireEntityOwnerApproval}
                onChange={(e: any) => setRequireEntityOwnerApproval(e.target.checked)}
                style={{ width: '16px', height: '16px', accentColor: colors.active }}
              />
              <span style={{ color: colors.heading, fontSize: '14px' }}>Require entity owner approval</span>
            </label>
            <div style={{ color: colors.muted, fontSize: '12px', marginLeft: '24px', marginTop: '4px' }}>
              When enabled, pull requests must be approved by the owners of all modified entities before merging.
            </div>
          </div>

          <div style={{ display: 'flex', gap: '8px' }}>
            <button onClick={handleSave} disabled={saving} style={{ ...btnPrimary, opacity: saving ? '0.6' : '1' }}>
              {saving ? 'Saving...' : 'Save protection rules'}
            </button>
            <button onClick={handleDeleteProtection} style={btnDanger}>
              Delete protection
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
