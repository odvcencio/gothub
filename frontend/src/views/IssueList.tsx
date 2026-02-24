import { useEffect, useState } from 'preact/hooks';
import { createIssue, getToken, listIssues } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
}

export function IssueListView({ owner, repo }: Props) {
  const [issues, setIssues] = useState<any[]>([]);
  const [state, setState] = useState<'open' | 'closed' | 'all'>('open');
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const loggedIn = !!getToken();

  const loadIssues = () => {
    if (!owner || !repo) return;
    setLoading(true);
    listIssues(owner, repo, state)
      .then(setIssues)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadIssues();
  }, [owner, repo, state]);

  const onCreateIssue = async (e: Event) => {
    e.preventDefault();
    if (!owner || !repo || !title.trim()) return;
    setSubmitting(true);
    setError('');
    try {
      const issue = await createIssue(owner, repo, { title, body });
      setTitle('');
      setBody('');
      setShowCreate(false);
      if (state === 'open' || state === 'all') {
        setIssues([issue, ...issues]);
      } else {
        loadIssues();
      }
    } catch (err: any) {
      setError(err.message || 'failed to create issue');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', flexWrap: 'wrap', marginBottom: '16px' }}>
        <h1 style={{ fontSize: '20px', color: '#f0f6fc', margin: 0 }}>Issues</h1>
        {loggedIn && (
          <button
            onClick={() => setShowCreate(!showCreate)}
            style={{ background: '#238636', color: '#fff', border: 'none', borderRadius: '6px', padding: '8px 12px', cursor: 'pointer', fontSize: '13px' }}
          >
            {showCreate ? 'Cancel' : 'New issue'}
          </button>
        )}
      </div>

      <div style={{ display: 'flex', gap: '8px', marginBottom: '12px' }}>
        {(['open', 'closed', 'all'] as const).map((s) => (
          <button
            key={s}
            onClick={() => setState(s)}
            style={{
              background: state === s ? '#1f6feb' : '#161b22',
              color: '#c9d1d9',
              border: '1px solid #30363d',
              borderRadius: '6px',
              padding: '6px 10px',
              cursor: 'pointer',
              fontSize: '13px',
            }}
          >
            {s}
          </button>
        ))}
      </div>

      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}

      {showCreate && loggedIn && (
        <form onSubmit={onCreateIssue} style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px', marginBottom: '12px' }}>
          <input
            value={title}
            onInput={(e: any) => setTitle(e.target.value)}
            placeholder="Title"
            style={{ width: '100%', marginBottom: '8px', boxSizing: 'border-box', background: '#0d1117', color: '#c9d1d9', border: '1px solid #30363d', borderRadius: '6px', padding: '8px 10px', fontSize: '14px' }}
          />
          <textarea
            value={body}
            onInput={(e: any) => setBody(e.target.value)}
            placeholder="Description (optional)"
            style={{ width: '100%', minHeight: '100px', boxSizing: 'border-box', background: '#0d1117', color: '#c9d1d9', border: '1px solid #30363d', borderRadius: '6px', padding: '8px 10px', fontSize: '13px', resize: 'vertical' }}
          />
          <div style={{ marginTop: '8px', display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="submit"
              disabled={submitting || !title.trim()}
              style={{ background: '#238636', color: '#fff', border: 'none', borderRadius: '6px', padding: '6px 12px', cursor: submitting ? 'not-allowed' : 'pointer' }}
            >
              {submitting ? 'Creating...' : 'Create issue'}
            </button>
          </div>
        </form>
      )}

      {loading ? (
        <div style={{ color: '#8b949e' }}>Loading...</div>
      ) : issues.length === 0 ? (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', color: '#8b949e' }}>No issues</div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {issues.map((issue, idx) => (
            <a
              key={issue.id}
              href={`/${owner}/${repo}/issues/${issue.number}`}
              style={{ display: 'block', textDecoration: 'none', padding: '12px 14px', borderTop: idx === 0 ? 'none' : '1px solid #30363d' }}
            >
              <div style={{ color: '#f0f6fc', fontSize: '14px', marginBottom: '4px' }}>
                #{issue.number} {issue.title}
              </div>
              <div style={{ color: '#8b949e', fontSize: '12px' }}>
                {issue.state} by {issue.author_name || 'user'}
              </div>
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
