import { useState, useEffect } from 'preact/hooks';
import { getEntityHistory } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

const inputStyle: Record<string, string> = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
};

export function EntityHistoryView({ owner, repo, ref: gitRef }: Props) {
  const params = new URLSearchParams(window.location.search);

  const [stableId, setStableId] = useState(params.get('stableId') || params.get('stable_id') || '');
  const [name, setName] = useState(params.get('name') || '');
  const [bodyHash, setBodyHash] = useState(params.get('bodyHash') || params.get('body_hash') || '');

  const [results, setResults] = useState<any[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [validationMsg, setValidationMsg] = useState('');

  const doSearch = () => {
    if (!owner || !repo || !gitRef) return;

    const trimmedStableId = stableId.trim();
    const trimmedName = name.trim();
    const trimmedBodyHash = bodyHash.trim();

    if (!trimmedStableId && !trimmedName && !trimmedBodyHash) {
      setValidationMsg('At least one search field must be filled.');
      return;
    }

    setValidationMsg('');
    setError('');
    setResults(null);
    setLoading(true);

    getEntityHistory(owner, repo, gitRef, {
      stableId: trimmedStableId || undefined,
      name: trimmedName || undefined,
      bodyHash: trimmedBodyHash || undefined,
    })
      .then((data) => {
        setResults(data || []);
      })
      .catch((e) => {
        setError(e.message || 'Failed to fetch entity history');
      })
      .finally(() => {
        setLoading(false);
      });
  };

  useEffect(() => {
    const trimmedStableId = stableId.trim();
    const trimmedName = name.trim();
    const trimmedBodyHash = bodyHash.trim();

    if (owner && repo && gitRef && (trimmedStableId || trimmedName || trimmedBodyHash)) {
      doSearch();
    }
  }, [owner, repo, gitRef]);

  const onSubmit = (e: Event) => {
    e.preventDefault();
    doSearch();
  };

  return (
    <div style={{ maxWidth: '900px' }}>
      <h2 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '4px' }}>
        Entity History
      </h2>
      <p style={{ color: '#8b949e', fontSize: '13px', marginTop: '0', marginBottom: '16px' }}>
        Track how a code entity has changed across commits in{' '}
        <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px', fontSize: '12px', color: '#c9d1d9' }}>
          {gitRef || 'HEAD'}
        </code>
      </p>

      <form onSubmit={onSubmit} style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', background: '#161b22', marginBottom: '20px' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '13px', marginBottom: '6px', fontWeight: '500' }}>
              Stable ID
            </label>
            <input
              type="text"
              value={stableId}
              onInput={(e: any) => setStableId(e.target.value)}
              placeholder="e.g. abc123def456"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
            />
          </div>

          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '13px', marginBottom: '6px', fontWeight: '500' }}>
              Entity Name
            </label>
            <input
              type="text"
              value={name}
              onInput={(e: any) => setName(e.target.value)}
              placeholder="e.g. MyFunction"
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
            />
          </div>

          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '13px', marginBottom: '6px', fontWeight: '500' }}>
              Body Hash
            </label>
            <input
              type="text"
              value={bodyHash}
              onInput={(e: any) => setBodyHash(e.target.value)}
              placeholder="e.g. sha256:..."
              style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' }}
            />
          </div>
        </div>

        {validationMsg && (
          <div style={{ color: '#f85149', fontSize: '13px', marginTop: '10px' }}>
            {validationMsg}
          </div>
        )}

        <div style={{ marginTop: '14px' }}>
          <button
            type="submit"
            disabled={loading}
            style={{
              background: '#238636',
              color: '#ffffff',
              border: 'none',
              borderRadius: '6px',
              padding: '8px 16px',
              fontSize: '14px',
              fontWeight: '500',
              cursor: loading ? 'not-allowed' : 'pointer',
              opacity: loading ? 0.7 : 1,
            }}
          >
            {loading ? 'Searching...' : 'Search'}
          </button>
        </div>
      </form>

      {error && (
        <div style={{ color: '#f85149', border: '1px solid #f8514933', borderRadius: '6px', padding: '12px 14px', marginBottom: '16px', background: '#f851490d', fontSize: '14px' }}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{ color: '#8b949e', padding: '20px 0', fontSize: '14px' }}>
          Loading...
        </div>
      )}

      {!loading && results !== null && results.length === 0 && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '24px', textAlign: 'center', color: '#8b949e', fontSize: '14px' }}>
          No history found for the given criteria.
        </div>
      )}

      {!loading && results !== null && results.length > 0 && (
        <div>
          <div style={{ color: '#8b949e', fontSize: '13px', marginBottom: '12px' }}>
            {results.length} result{results.length !== 1 ? 's' : ''}
          </div>

          <div style={{ position: 'relative', paddingLeft: '24px' }}>
            {/* Timeline line */}
            <div style={{
              position: 'absolute',
              left: '7px',
              top: '0',
              bottom: '0',
              width: '2px',
              background: '#30363d',
            }} />

            {results.map((entry, idx) => (
              <div key={`${entry.commit_hash}-${entry.stable_id}-${idx}`} style={{ position: 'relative', marginBottom: '16px' }}>
                {/* Timeline dot */}
                <div style={{
                  position: 'absolute',
                  left: '-20px',
                  top: '18px',
                  width: '10px',
                  height: '10px',
                  borderRadius: '50%',
                  background: '#1f6feb',
                  border: '2px solid #0d1117',
                }} />

                <div style={{
                  border: '1px solid #30363d',
                  borderRadius: '6px',
                  background: '#161b22',
                  overflow: 'hidden',
                }}>
                  {/* Commit header */}
                  <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d' }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '8px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                        <a
                          href={`/${owner}/${repo}/commit/${entry.commit_hash}`}
                          style={{
                            fontFamily: 'monospace',
                            fontSize: '13px',
                            color: '#58a6ff',
                            textDecoration: 'none',
                            background: '#0d1117',
                            padding: '2px 8px',
                            borderRadius: '4px',
                            border: '1px solid #30363d',
                          }}
                        >
                          {entry.commit_hash?.slice(0, 7)}
                        </a>
                        <span style={{ color: '#c9d1d9', fontSize: '14px', fontWeight: '500' }}>
                          {entry.author}
                        </span>
                      </div>
                      <span style={{ color: '#8b949e', fontSize: '12px', flexShrink: 0 }}>
                        {formatTimestamp(entry.timestamp)}
                      </span>
                    </div>
                    {entry.message && (
                      <div style={{ color: '#f0f6fc', fontSize: '14px', marginTop: '8px', lineHeight: '1.4' }}>
                        {entry.message}
                      </div>
                    )}
                  </div>

                  {/* Entity info */}
                  <div style={{ padding: '10px 16px', display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '8px' }}>
                    {entry.kind && (
                      <span style={{
                        display: 'inline-block',
                        background: '#1f6feb',
                        color: '#ffffff',
                        fontSize: '11px',
                        fontWeight: '600',
                        padding: '2px 8px',
                        borderRadius: '10px',
                        textTransform: 'uppercase',
                        letterSpacing: '0.5px',
                      }}>
                        {entry.kind}
                      </span>
                    )}
                    {entry.decl_kind && (
                      <span style={{
                        display: 'inline-block',
                        background: '#30363d',
                        color: '#c9d1d9',
                        fontSize: '11px',
                        padding: '2px 8px',
                        borderRadius: '10px',
                      }}>
                        {entry.decl_kind}
                      </span>
                    )}
                    {entry.name && (
                      <span style={{ color: '#f0f6fc', fontSize: '14px', fontFamily: 'monospace', fontWeight: '500' }}>
                        {entry.receiver ? `${entry.receiver}.` : ''}{entry.name}
                      </span>
                    )}
                    {entry.path && (
                      <span style={{ color: '#8b949e', fontSize: '12px', marginLeft: 'auto' }}>
                        {entry.path}
                      </span>
                    )}
                  </div>

                  {/* Hashes footer */}
                  <div style={{ padding: '8px 16px', borderTop: '1px solid #30363d', display: 'flex', gap: '16px', flexWrap: 'wrap' }}>
                    {entry.stable_id && (
                      <span style={{ color: '#8b949e', fontSize: '12px' }}>
                        ID:{' '}
                        <span style={{ fontFamily: 'monospace' }}>
                          {entry.stable_id.length > 16 ? entry.stable_id.slice(0, 16) + '...' : entry.stable_id}
                        </span>
                      </span>
                    )}
                    {entry.entity_hash && (
                      <span style={{ color: '#8b949e', fontSize: '12px' }}>
                        Entity:{' '}
                        <span style={{ fontFamily: 'monospace' }}>
                          {entry.entity_hash.length > 16 ? entry.entity_hash.slice(0, 16) + '...' : entry.entity_hash}
                        </span>
                      </span>
                    )}
                    {entry.body_hash && (
                      <span style={{ color: '#8b949e', fontSize: '12px' }}>
                        Body:{' '}
                        <span style={{ fontFamily: 'monospace' }}>
                          {entry.body_hash.length > 16 ? entry.body_hash.slice(0, 16) + '...' : entry.body_hash}
                        </span>
                      </span>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function formatTimestamp(ts: unknown): string {
  if (!ts) return '';
  const d = toDate(ts);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const diffMs = now.getTime() - d.getTime();
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin} minute${diffMin !== 1 ? 's' : ''} ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr} hour${diffHr !== 1 ? 's' : ''} ago`;
  const diffDays = Math.floor(diffHr / 24);
  if (diffDays === 1) return 'yesterday';
  if (diffDays < 30) return `${diffDays} days ago`;
  return d.toLocaleDateString();
}

function toDate(ts: unknown): Date {
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
