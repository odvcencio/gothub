import { useState, useEffect } from 'preact/hooks';
import { listCommits } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
}

export function CommitsView({ owner, repo, ref: gitRef }: Props) {
  const [commits, setCommits] = useState<any[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo || !gitRef) return;
    listCommits(owner, repo, gitRef).then(setCommits).catch(e => setError(e.message));
  }, [owner, repo, gitRef]);

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  return (
    <div>
      <h2 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '16px' }}>
        Commits on <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px', fontSize: '16px' }}>{gitRef}</code>
      </h2>

      {commits.length === 0 ? (
        <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {commits.map(c => (
            <div key={c.hash} style={{ padding: '12px 16px', borderBottom: '1px solid #21262d', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div>
                <div style={{ color: '#f0f6fc', fontSize: '14px', marginBottom: '4px' }}>{c.message}</div>
                <div style={{ color: '#8b949e', fontSize: '12px' }}>
                  {c.author} committed {formatDate(c.timestamp)}
                </div>
              </div>
              <div style={{ display: 'flex', gap: '8px', flexShrink: 0 }}>
                <a href={`/${owner}/${repo}/tree/${c.hash}`}
                  style={{ fontFamily: 'monospace', fontSize: '12px', color: '#58a6ff', border: '1px solid #30363d', padding: '4px 8px', borderRadius: '6px' }}>
                  {c.hash?.slice(0, 10)}
                </a>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function formatDate(ts: unknown): string {
  if (!ts) return '';
  const d = commitTimestampToDate(ts);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const diff = now.getTime() - d.getTime();
  const days = Math.floor(diff / 86400000);
  if (days === 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return d.toLocaleDateString();
}

function commitTimestampToDate(ts: unknown): Date {
  if (typeof ts === 'number') {
    return new Date(ts < 1e12 ? ts * 1000 : ts);
  }
  if (typeof ts === 'string') {
    const n = Number(ts);
    if (!Number.isNaN(n)) {
      return new Date(n < 1e12 ? n * 1000 : n);
    }
    return new Date(ts);
  }
  return new Date(NaN);
}
