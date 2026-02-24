import { useState, useEffect } from 'preact/hooks';
import { listPRs, getToken } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
}

export function PRListView({ owner, repo }: Props) {
  const [prs, setPrs] = useState<any[]>([]);
  const [filter, setFilter] = useState<'open' | 'closed' | 'merged'>('open');
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo) return;
    listPRs(owner, repo, filter).then(setPrs).catch(e => setError(e.message));
  }, [owner, repo, filter]);

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
        <div style={{ display: 'flex', gap: '8px' }}>
          {(['open', 'closed', 'merged'] as const).map(s => (
            <button key={s} onClick={() => setFilter(s)}
              style={{
                background: filter === s ? '#30363d' : 'transparent',
                color: filter === s ? '#f0f6fc' : '#8b949e',
                border: '1px solid #30363d',
                padding: '6px 12px',
                borderRadius: '6px',
                cursor: 'pointer',
                fontSize: '13px',
              }}>
              {s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>
        {getToken() && (
          <a href={`/${owner}/${repo}/pulls/new`}
            style={{ background: '#238636', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: '6px', fontWeight: 'bold', fontSize: '13px' }}>
            New pull request
          </a>
        )}
      </div>

      {prs.length === 0 ? (
        <div style={{ color: '#8b949e', padding: '40px', textAlign: 'center', border: '1px solid #30363d', borderRadius: '6px' }}>
          No {filter} pull requests
        </div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {prs.map(pr => (
            <a key={pr.id} href={`/${owner}/${repo}/pulls/${pr.number}`}
              style={{ display: 'block', padding: '12px 16px', borderBottom: '1px solid #21262d' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ color: stateColor(pr.state), fontSize: '14px' }}>
                  {stateIcon(pr.state)}
                </span>
                <span style={{ color: '#f0f6fc', fontSize: '14px', fontWeight: 'bold' }}>{pr.title}</span>
              </div>
              <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '4px' }}>
                #{pr.number} opened by {pr.author_name || 'unknown'} &middot; {pr.source_branch} → {pr.target_branch}
              </div>
            </a>
          ))}
        </div>
      )}
    </div>
  );
}

function stateColor(state: string): string {
  switch (state) {
    case 'open': return '#3fb950';
    case 'merged': return '#a371f7';
    case 'closed': return '#f85149';
    default: return '#8b949e';
  }
}

function stateIcon(state: string): string {
  switch (state) {
    case 'open': return '\u25CB';
    case 'merged': return '\u25C9';
    case 'closed': return '\u25CF';
    default: return '\u25CB';
  }
}
