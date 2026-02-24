import { useState, useEffect } from 'preact/hooks';
import { highlight, extractEntities, HighlightRange, EntityInfo } from '../wasm/loader';

interface Props {
  filename: string;
  source: string;
}

export function CodeViewer({ filename, source }: Props) {
  const [ranges, setRanges] = useState<HighlightRange[]>([]);
  const [entities, setEntities] = useState<EntityInfo[]>([]);
  const [error, setError] = useState<string>('');

  useEffect(() => {
    highlight(filename, source).then(setRanges).catch(e => setError(e.message));
    extractEntities(filename, source).then(setEntities).catch(() => {});
  }, [filename, source]);

  const lines = source.split('\n');

  return (
    <div style={{ display: 'flex', gap: '16px' }}>
      <div style={{ flex: 1, overflow: 'auto' }}>
        <pre style={{ background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '16px', fontSize: '13px', lineHeight: '1.5', overflowX: 'auto' }}>
          <code>
            {error ? source : renderHighlighted(source, ranges)}
          </code>
        </pre>
      </div>
      {entities.length > 0 && (
        <div style={{ width: '250px', flexShrink: 0 }}>
          <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px' }}>
            <h3 style={{ fontSize: '14px', marginBottom: '8px', color: '#f0f6fc' }}>Entities</h3>
            {entities.filter(e => e.kind === 'declaration').map(e => (
              <div key={e.key} style={{ padding: '4px 0', fontSize: '13px', fontFamily: 'monospace' }}>
                <span style={{ color: '#8b949e' }}>{e.decl_kind} </span>
                <span style={{ color: '#d2a8ff' }}>{e.receiver ? e.receiver + '.' : ''}{e.name}</span>
                <span style={{ color: '#484f58', fontSize: '11px' }}> L{e.start_line}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

const captureColors: Record<string, string> = {
  keyword: '#ff7b72',
  string: '#a5d6ff',
  comment: '#8b949e',
  function: '#d2a8ff',
  'function.method': '#d2a8ff',
  type: '#79c0ff',
  number: '#79c0ff',
  operator: '#ff7b72',
  variable: '#ffa657',
  'variable.parameter': '#ffa657',
  'variable.builtin': '#ffa657',
  constant: '#79c0ff',
  'constant.builtin': '#79c0ff',
  property: '#c9d1d9',
  punctuation: '#c9d1d9',
  'punctuation.bracket': '#c9d1d9',
  tag: '#7ee787',
  attribute: '#79c0ff',
};

function renderHighlighted(source: string, ranges: HighlightRange[]): (string | any)[] {
  if (ranges.length === 0) return [source];

  const result: any[] = [];
  let pos = 0;

  for (const r of ranges) {
    if (r.start_byte > pos) {
      result.push(source.slice(pos, r.start_byte));
    }
    const color = captureColors[r.capture] || '#c9d1d9';
    result.push(
      <span style={{ color }}>{source.slice(r.start_byte, r.end_byte)}</span>
    );
    pos = r.end_byte;
  }

  if (pos < source.length) {
    result.push(source.slice(pos));
  }

  return result;
}
