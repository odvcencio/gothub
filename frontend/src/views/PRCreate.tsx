import { useEffect, useState } from 'preact/hooks';
import { createPR, getRepo, getToken, listBranches } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
}

export function PRCreateView({ owner, repo }: Props) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [branches, setBranches] = useState<string[]>([]);
  const [sourceBranch, setSourceBranch] = useState('');
  const [targetBranch, setTargetBranch] = useState('main');
  const [submitting, setSubmitting] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo) return;
    Promise.all([listBranches(owner, repo), getRepo(owner, repo)])
      .then(([bs, repoInfo]) => {
        setBranches(bs);
        const target = repoInfo.default_branch || 'main';
        setTargetBranch(target);

        const source = bs.find(b => b !== target) || bs[0] || '';
        setSourceBranch(source);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [owner, repo]);

  const onSubmit = async (e: Event) => {
    e.preventDefault();
    if (!owner || !repo) return;
    if (!title.trim() || !sourceBranch) {
      setError('Title and source branch are required');
      return;
    }
    if (sourceBranch === targetBranch) {
      setError('Source and target branch must be different');
      return;
    }

    setSubmitting(true);
    setError('');
    try {
      const pr = await createPR(owner, repo, {
        title,
        body,
        source_branch: sourceBranch,
        target_branch: targetBranch,
      });
      location.href = `/${owner}/${repo}/pulls/${pr.number}`;
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  };

  if (!getToken()) {
    return <div style={{ color: '#8b949e', padding: '20px' }}>Sign in to create a pull request.</div>;
  }
  if (loading) {
    return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;
  }
  if (error && branches.length === 0) {
    return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  }
  if (branches.length === 0) {
    return <div style={{ color: '#8b949e', padding: '20px' }}>No branches found. Push at least one branch first.</div>;
  }

  return (
    <div style={{ maxWidth: '800px' }}>
      <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '16px' }}>
        Open a new pull request
      </h1>

      {error && (
        <div style={{ color: '#f85149', marginBottom: '12px', border: '1px solid #30363d', padding: '10px', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      <form onSubmit={onSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <input
          value={title}
          onInput={(e: any) => setTitle(e.target.value)}
          placeholder="Title"
          style={inputStyle}
        />

        <div style={{ display: 'flex', gap: '12px', flexWrap: 'wrap' }}>
          <label style={labelStyle}>
            Source
            <select value={sourceBranch} onChange={(e: any) => setSourceBranch(e.target.value)} style={inputStyle}>
              {branches.map(b => (
                <option key={b} value={b}>{b}</option>
              ))}
            </select>
          </label>

          <label style={labelStyle}>
            Target
            <select value={targetBranch} onChange={(e: any) => setTargetBranch(e.target.value)} style={inputStyle}>
              {branches.map(b => (
                <option key={b} value={b}>{b}</option>
              ))}
            </select>
          </label>
        </div>

        <textarea
          value={body}
          onInput={(e: any) => setBody(e.target.value)}
          placeholder="Description (optional)"
          style={{ ...inputStyle, minHeight: '150px', resize: 'vertical' }}
        />

        <button
          type="submit"
          disabled={submitting}
          style={{
            background: submitting ? '#1f6f3d' : '#238636',
            color: '#fff',
            border: 'none',
            padding: '10px 16px',
            borderRadius: '6px',
            cursor: submitting ? 'not-allowed' : 'pointer',
            fontWeight: 'bold',
            alignSelf: 'flex-start',
          }}
        >
          {submitting ? 'Creating...' : 'Create pull request'}
        </button>
      </form>
    </div>
  );
}

const inputStyle = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
  width: '100%',
  boxSizing: 'border-box',
};

const labelStyle = {
  color: '#c9d1d9',
  fontSize: '13px',
  display: 'flex',
  flexDirection: 'column' as const,
  gap: '6px',
  minWidth: '220px',
  flex: 1,
};
