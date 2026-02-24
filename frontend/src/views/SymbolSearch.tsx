import { useState, useEffect, useRef } from 'preact/hooks';
import { searchSymbols } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  ref?: string;
  path?: string;
}

const COLORS = {
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
};

const inputStyle: Record<string, string> = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
};

const KIND_COLORS: Record<string, string> = {
  function: '#58a6ff',
  type: '#238636',
  method: '#a371f7',
  variable: '#d29922',
  constant: '#e3b341',
};

function kindColor(kind: string): string {
  const lower = kind.toLowerCase();
  return KIND_COLORS[lower] || COLORS.muted;
}

export function SymbolSearchView({ owner, repo, ref: gitRef, path }: Props) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [searched, setSearched] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!owner || !repo || !gitRef) return;

    if (timerRef.current) clearTimeout(timerRef.current);

    if (!query.trim()) {
      setResults([]);
      setSearched(false);
      setError('');
      return;
    }

    timerRef.current = setTimeout(() => {
      setLoading(true);
      setError('');
      setSearched(true);
      searchSymbols(owner, repo, gitRef, query.trim())
        .then((data) => {
          setResults(data || []);
        })
        .catch((e) => {
          setError(e.message || 'Search failed');
          setResults([]);
        })
        .finally(() => setLoading(false));
    }, 300);

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [query, owner, repo, gitRef]);

  return (
    <div style={{ color: COLORS.text }}>
      <h1 style={{ fontSize: '20px', color: COLORS.heading, margin: '0 0 16px 0' }}>
        Symbol Search
      </h1>

      <div style={{ marginBottom: '16px' }}>
        <input
          type="text"
          placeholder="Search symbols..."
          value={query}
          onInput={(e: any) => setQuery(e.target.value)}
          style={{ ...inputStyle, width: '100%', boxSizing: 'border-box' as any }}
        />
      </div>

      {error && (
        <div style={{ color: COLORS.red, marginBottom: '12px', fontSize: '14px' }}>
          {error}
        </div>
      )}

      {loading && (
        <div style={{ color: COLORS.muted, padding: '20px 0', fontSize: '14px' }}>
          Searching...
        </div>
      )}

      {!loading && searched && results.length === 0 && !error && (
        <div
          style={{
            border: `1px solid ${COLORS.border}`,
            borderRadius: '6px',
            padding: '24px',
            color: COLORS.muted,
            textAlign: 'center',
            fontSize: '14px',
          }}
        >
          No symbols found for "{query}"
        </div>
      )}

      {!loading && results.length > 0 && (
        <div>
          <div style={{ color: COLORS.muted, fontSize: '13px', marginBottom: '8px' }}>
            {results.length} result{results.length !== 1 ? 's' : ''}
          </div>
          <div style={{ border: `1px solid ${COLORS.border}`, borderRadius: '6px' }}>
            {results.map((sym, idx) => (
              <a
                key={`${sym.file}-${sym.name}-${sym.start_line}-${idx}`}
                href={`/${owner}/${repo}/blob/${gitRef}/${sym.file}`}
                style={{
                  display: 'block',
                  textDecoration: 'none',
                  padding: '12px 14px',
                  borderTop: idx === 0 ? 'none' : `1px solid ${COLORS.border}`,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                  <span
                    style={{
                      display: 'inline-block',
                      padding: '2px 8px',
                      borderRadius: '12px',
                      fontSize: '11px',
                      fontWeight: 'bold',
                      color: kindColor(sym.kind),
                      border: `1px solid ${kindColor(sym.kind)}`,
                      textTransform: 'lowercase',
                      flexShrink: 0,
                    }}
                  >
                    {sym.kind}
                  </span>
                  <span style={{ color: COLORS.heading, fontWeight: 'bold', fontSize: '14px' }}>
                    {sym.name}
                  </span>
                  {sym.receiver && (
                    <span style={{ color: COLORS.muted, fontSize: '13px' }}>
                      on {sym.receiver}
                    </span>
                  )}
                </div>
                {sym.signature && (
                  <div style={{ color: COLORS.muted, fontSize: '13px', marginTop: '4px', fontFamily: 'monospace' }}>
                    {sym.signature}
                  </div>
                )}
                <div style={{ color: COLORS.link, fontSize: '12px', marginTop: '4px' }}>
                  {sym.file}
                  <span style={{ color: COLORS.muted }}>
                    :{sym.start_line}
                    {sym.end_line && sym.end_line !== sym.start_line ? `-${sym.end_line}` : ''}
                  </span>
                </div>
              </a>
            ))}
          </div>
        </div>
      )}

      {!loading && !searched && !error && (
        <div style={{ color: COLORS.muted, fontSize: '14px', padding: '20px 0' }}>
          Type a query to search for symbols in this repository.
        </div>
      )}
    </div>
  );
}
