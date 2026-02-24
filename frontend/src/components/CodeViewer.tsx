import type { ComponentChildren, JSX } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import {
  findReferences,
  getEntityHistory,
  searchSymbols,
  type EntityHistoryHit,
  type ReferenceResult,
  type SymbolResult,
} from '../api/client';
import { highlight, HighlightRange } from '../wasm/loader';

interface Props {
  filename: string;
  source: string;
  owner?: string;
  repo?: string;
  gitRef?: string;
  path?: string;
}

interface SymbolToken {
  name: string;
  start: number;
  end: number;
  line: number;
  column: number;
}

interface SymbolInsight {
  loading: boolean;
  resolved: boolean;
  definitions: SymbolResult[];
  references: ReferenceResult[];
  history: EntityHistoryHit[];
  error?: string;
}

const NON_SYMBOL_CAPTURE_ROOTS = new Set([
  'keyword',
  'string',
  'comment',
  'number',
  'operator',
  'punctuation',
  'tag',
  'attribute',
]);

const IDENTIFIER_PATTERN = /^[A-Za-z_$][A-Za-z0-9_$]*$/;
const MAX_INTERACTIVE_SOURCE_LENGTH = 180_000;
const MAX_INTERACTIVE_RANGES = 12_000;
const HOVER_FETCH_DELAY_MS = 220;
const MIN_SYMBOL_LENGTH = 2;
const MAX_PANEL_DEFINITIONS = 8;
const MAX_PANEL_REFERENCES = 16;
const MAX_PANEL_HISTORY = 6;

export function CodeViewer({ filename, source, owner, repo, gitRef, path }: Props) {
  const [ranges, setRanges] = useState<HighlightRange[]>([]);
  const [error, setError] = useState<string>('');
  const [activeToken, setActiveToken] = useState<SymbolToken | null>(null);
  const [pinned, setPinned] = useState(false);
  const [insights, setInsights] = useState<Record<string, SymbolInsight>>({});

  const currentPath = path || filename;
  const lineStarts = useMemo(() => buildLineStarts(source), [source]);

  useEffect(() => {
    let cancelled = false;
    setError('');
    setRanges([]);

    highlight(filename, source)
      .then((nextRanges) => {
        if (!cancelled) setRanges(nextRanges);
      })
      .catch((e) => {
        if (!cancelled) setError(e.message || 'failed to highlight source');
      });

    return () => {
      cancelled = true;
    };
  }, [filename, source]);

  useEffect(() => {
    setActiveToken(null);
    setPinned(false);
    setInsights({});
  }, [filename, source, owner, repo, gitRef, currentPath]);

  const interactiveDisabledReason = useMemo(() => {
    if (!owner || !repo || !gitRef) {
      return 'Repository context is missing for inline code intelligence.';
    }
    if (!currentPath) {
      return 'File path is missing for inline code intelligence.';
    }
    if (source.length > MAX_INTERACTIVE_SOURCE_LENGTH) {
      return `Inline code intelligence is disabled for files larger than ${MAX_INTERACTIVE_SOURCE_LENGTH.toLocaleString()} characters.`;
    }
    if (ranges.length > MAX_INTERACTIVE_RANGES) {
      return `Inline code intelligence is disabled for files with more than ${MAX_INTERACTIVE_RANGES.toLocaleString()} highlighted tokens.`;
    }
    return '';
  }, [owner, repo, gitRef, currentPath, source.length, ranges.length]);

  const intelligenceEnabled = interactiveDisabledReason === '';
  const activeInsight = activeToken ? insights[activeToken.name] : undefined;

  useEffect(() => {
    if (!activeToken || !owner || !repo || !gitRef || !intelligenceEnabled) return;
    if (activeToken.name.length < MIN_SYMBOL_LENGTH) return;
    if (activeInsight?.loading || activeInsight?.resolved) return;

    let cancelled = false;
    const symbol = activeToken.name;

    const timer = window.setTimeout(async () => {
      setInsights((prev) => ({
        ...prev,
        [symbol]: {
          loading: true,
          resolved: false,
          definitions: prev[symbol]?.definitions || [],
          references: prev[symbol]?.references || [],
          history: prev[symbol]?.history || [],
        },
      }));

      const selector = `*[name=/^${escapeRegexForSelector(symbol)}$/]`;
      const [definitionsResult, referencesResult, historyResult] = await Promise.allSettled([
        searchSymbols(owner, repo, gitRef, selector),
        findReferences(owner, repo, gitRef, symbol),
        getEntityHistory(owner, repo, gitRef, { name: symbol, limit: 40 }),
      ]);

      if (cancelled) return;

      const definitions = definitionsResult.status === 'fulfilled' ? definitionsResult.value || [] : [];
      const references = referencesResult.status === 'fulfilled' ? referencesResult.value || [] : [];
      const history = historyResult.status === 'fulfilled' ? historyResult.value || [] : [];

      const failedParts: string[] = [];
      if (definitionsResult.status === 'rejected') failedParts.push('definitions');
      if (referencesResult.status === 'rejected') failedParts.push('references');
      if (historyResult.status === 'rejected') failedParts.push('entity history');

      setInsights((prev) => ({
        ...prev,
        [symbol]: {
          loading: false,
          resolved: true,
          definitions,
          references,
          history,
          error: failedParts.length > 0 ? `Unable to load ${failedParts.join(', ')}.` : undefined,
        },
      }));
    }, pinned ? 0 : HOVER_FETCH_DELAY_MS);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [
    activeToken?.name,
    owner,
    repo,
    gitRef,
    intelligenceEnabled,
    pinned,
    activeInsight?.loading,
    activeInsight?.resolved,
  ]);

  const definitions = useMemo(() => {
    const items = activeInsight?.definitions || [];
    return prioritizeDefinitions(items, currentPath, activeToken?.line || 0);
  }, [activeInsight?.definitions, currentPath, activeToken?.line]);

  const references = useMemo(() => {
    const items = activeInsight?.references || [];
    return prioritizeByCurrentFile(items, currentPath);
  }, [activeInsight?.references, currentPath]);

  const history = activeInsight?.history || [];

  const handleCodeMouseOver = (event: JSX.TargetedMouseEvent<HTMLElement>) => {
    if (pinned || !intelligenceEnabled) return;

    const tokenEl = closestSymbolElement(event.target);
    if (!tokenEl) {
      setActiveToken((prev) => (prev ? null : prev));
      return;
    }

    const token = tokenFromElement(tokenEl, lineStarts);
    if (!token) return;
    setActiveToken((prev) => (sameToken(prev, token) ? prev : token));
  };

  const handleCodeClick = (event: JSX.TargetedMouseEvent<HTMLElement>) => {
    if (!intelligenceEnabled) return;

    const tokenEl = closestSymbolElement(event.target);
    if (!tokenEl) return;

    event.preventDefault();
    event.stopPropagation();

    const token = tokenFromElement(tokenEl, lineStarts);
    if (!token) return;

    setActiveToken(token);
    setPinned(true);
  };

  const clearSelection = () => {
    setPinned(false);
    setActiveToken(null);
  };

  return (
    <div style={{ display: 'flex', gap: '16px', alignItems: 'flex-start' }}>
      <div style={{ flex: 1, overflow: 'auto', minWidth: 0 }}>
        <pre style={{ background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '16px', fontSize: '13px', lineHeight: '1.5', overflowX: 'auto' }}>
          <code
            onMouseOver={handleCodeMouseOver}
            onClick={handleCodeClick}
            onMouseLeave={() => {
              if (!pinned) setActiveToken(null);
            }}
          >
            {error ? source : renderHighlighted(source, ranges, { activeToken, intelligenceEnabled })}
          </code>
        </pre>
      </div>

      <aside style={{ width: '320px', maxWidth: '100%', flexShrink: 0, border: '1px solid #30363d', borderRadius: '6px', overflow: 'hidden' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '10px 12px', borderBottom: '1px solid #30363d', background: '#161b22' }}>
          <strong style={{ color: '#f0f6fc', fontSize: '13px' }}>Inline Intelligence</strong>
          {(activeToken || pinned) && (
            <button
              type="button"
              onClick={clearSelection}
              style={{
                background: '#21262d',
                color: '#c9d1d9',
                border: '1px solid #30363d',
                borderRadius: '4px',
                fontSize: '11px',
                padding: '2px 8px',
                cursor: 'pointer',
              }}
            >
              Clear
            </button>
          )}
        </div>

        <div style={{ padding: '12px', display: 'grid', gap: '10px' }}>
          {interactiveDisabledReason && (
            <div style={{ color: '#8b949e', fontSize: '13px' }}>{interactiveDisabledReason}</div>
          )}

          {!interactiveDisabledReason && !activeToken && (
            <div style={{ color: '#8b949e', fontSize: '13px' }}>
              Hover a symbol token to preview definitions and references, or click to pin.
            </div>
          )}

          {!interactiveDisabledReason && activeToken && (
            <>
              <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '8px 10px', background: '#0d1117' }}>
                <div style={{ color: '#58a6ff', fontFamily: 'monospace', fontSize: '13px', wordBreak: 'break-word' }}>
                  {activeToken.name}
                </div>
                <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '4px' }}>
                  L{activeToken.line}:{activeToken.column} {pinned ? '\u00b7 pinned' : '\u00b7 hover'}
                </div>
              </div>

              {activeToken.name.length < MIN_SYMBOL_LENGTH && (
                <div style={{ color: '#8b949e', fontSize: '12px' }}>
                  Symbol is too short for repository-wide lookups.
                </div>
              )}

              {activeToken.name.length >= MIN_SYMBOL_LENGTH && activeInsight?.loading && (
                <div style={{ color: '#8b949e', fontSize: '12px' }}>
                  Loading definitions, references, and entity history...
                </div>
              )}

              {activeToken.name.length >= MIN_SYMBOL_LENGTH && activeInsight?.error && (
                <div style={{ color: '#d29922', fontSize: '12px' }}>{activeInsight.error}</div>
              )}

              {activeToken.name.length >= MIN_SYMBOL_LENGTH && (
                <>
                  <PanelSection title={`Definitions (${definitions.length})`}>
                    {definitions.slice(0, MAX_PANEL_DEFINITIONS).map((definition, idx) => (
                      <a
                        key={`${definition.file}:${definition.start_line || 0}:${idx}`}
                        href={buildBlobHref(owner, repo, gitRef, definition.file, definition.start_line)}
                        style={{ display: 'block', color: '#58a6ff', textDecoration: 'none', padding: '4px 0' }}
                      >
                        <div style={{ fontSize: '12px', fontFamily: 'monospace', color: '#c9d1d9' }}>
                          {definition.file}
                        </div>
                        <div style={{ fontSize: '11px', color: '#8b949e' }}>
                          {definition.kind || 'symbol'} {formatLineRange(definition.start_line, definition.end_line)}
                        </div>
                      </a>
                    ))}
                    {definitions.length === 0 && activeInsight?.resolved && (
                      <div style={{ color: '#8b949e', fontSize: '12px' }}>No matching definitions found.</div>
                    )}
                    {definitions.length > MAX_PANEL_DEFINITIONS && (
                      <div style={{ color: '#8b949e', fontSize: '11px' }}>
                        +{definitions.length - MAX_PANEL_DEFINITIONS} more definitions
                      </div>
                    )}
                  </PanelSection>

                  <PanelSection title={`References (${references.length})`}>
                    {references.slice(0, MAX_PANEL_REFERENCES).map((reference, idx) => (
                      <a
                        key={`${reference.file}:${reference.start_line || reference.line || 0}:${reference.start_column || 0}:${idx}`}
                        href={buildBlobHref(owner, repo, gitRef, reference.file, reference.start_line || reference.line)}
                        style={{ display: 'block', color: '#58a6ff', textDecoration: 'none', padding: '4px 0' }}
                      >
                        <div style={{ fontSize: '12px', fontFamily: 'monospace', color: '#c9d1d9' }}>
                          {reference.file}
                        </div>
                        <div style={{ fontSize: '11px', color: '#8b949e' }}>
                          {reference.kind || 'reference'} {formatReferenceLocation(reference)}
                        </div>
                      </a>
                    ))}
                    {references.length === 0 && activeInsight?.resolved && (
                      <div style={{ color: '#8b949e', fontSize: '12px' }}>No matching references found.</div>
                    )}
                    {references.length > MAX_PANEL_REFERENCES && (
                      <div style={{ color: '#8b949e', fontSize: '11px' }}>
                        +{references.length - MAX_PANEL_REFERENCES} more references
                      </div>
                    )}
                  </PanelSection>

                  <PanelSection title={`Entity History (${history.length})`}>
                    {history.slice(0, MAX_PANEL_HISTORY).map((entry, idx) => (
                      <a
                        key={`${entry.commit_hash}:${entry.path}:${idx}`}
                        href={buildBlobHref(owner, repo, gitRef, entry.path)}
                        style={{ display: 'block', color: '#58a6ff', textDecoration: 'none', padding: '4px 0' }}
                      >
                        <div style={{ color: '#58a6ff', fontFamily: 'monospace', fontSize: '11px' }}>
                          {shortHash(entry.commit_hash)}
                        </div>
                        <div style={{ fontSize: '12px', color: '#c9d1d9' }}>{entry.path}</div>
                        <div style={{ fontSize: '11px', color: '#8b949e' }}>
                          {entry.author || 'unknown'} {entry.timestamp ? `\u00b7 ${formatTimestamp(entry.timestamp)}` : ''}
                        </div>
                      </a>
                    ))}
                    {history.length === 0 && activeInsight?.resolved && (
                      <div style={{ color: '#8b949e', fontSize: '12px' }}>No related entity history found.</div>
                    )}
                    {history.length > MAX_PANEL_HISTORY && (
                      <div style={{ color: '#8b949e', fontSize: '11px' }}>
                        +{history.length - MAX_PANEL_HISTORY} more history hits
                      </div>
                    )}
                  </PanelSection>
                </>
              )}
            </>
          )}
        </div>
      </aside>
    </div>
  );
}

function PanelSection({ title, children }: { title: string; children: ComponentChildren }) {
  return (
    <section style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '8px 10px', background: '#0d1117' }}>
      <div style={{ color: '#f0f6fc', fontSize: '12px', marginBottom: '6px' }}>{title}</div>
      <div>{children}</div>
    </section>
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

function renderHighlighted(
  source: string,
  ranges: HighlightRange[],
  options: {
    activeToken: SymbolToken | null;
    intelligenceEnabled: boolean;
  },
): Array<string | JSX.Element> {
  if (ranges.length === 0) return [source];

  const result: Array<string | JSX.Element> = [];
  let pos = 0;

  for (const r of ranges) {
    if (r.start_byte > pos) {
      result.push(source.slice(pos, r.start_byte));
    }

    const token = source.slice(r.start_byte, r.end_byte);
    const color = captureColors[r.capture] || '#c9d1d9';
    const interactive = options.intelligenceEnabled && isSymbolToken(r.capture, token);
    const isActive = !!options.activeToken
      && options.activeToken.start === r.start_byte
      && options.activeToken.end === r.end_byte;

    result.push(
      <span
        data-symbol={interactive ? token : undefined}
        data-start={interactive ? String(r.start_byte) : undefined}
        data-end={interactive ? String(r.end_byte) : undefined}
        style={{
          color,
          cursor: interactive ? 'pointer' : 'text',
          borderRadius: interactive ? '3px' : undefined,
          background: isActive ? 'rgba(31, 111, 235, 0.18)' : undefined,
          outline: isActive ? '1px solid rgba(31, 111, 235, 0.45)' : undefined,
        }}
      >
        {token}
      </span>,
    );

    pos = r.end_byte;
  }

  if (pos < source.length) {
    result.push(source.slice(pos));
  }

  return result;
}

function isSymbolToken(capture: string, token: string): boolean {
  const root = capture.split('.')[0];
  if (NON_SYMBOL_CAPTURE_ROOTS.has(root)) return false;
  if (!IDENTIFIER_PATTERN.test(token)) return false;
  return token.length >= MIN_SYMBOL_LENGTH;
}

function escapeRegexForSelector(value: string): string {
  return value.replace(/[\\^$.*+?()[\]{}|]/g, '\\$&');
}

function closestSymbolElement(target: EventTarget | null): HTMLElement | null {
  if (!target) return null;

  let el: HTMLElement | null = null;
  if (target instanceof HTMLElement) {
    el = target;
  } else if (target instanceof Text) {
    el = target.parentElement;
  }

  if (!el) return null;
  return el.closest('[data-symbol]') as HTMLElement | null;
}

function tokenFromElement(el: HTMLElement, lineStarts: number[]): SymbolToken | null {
  const name = el.dataset.symbol;
  const startRaw = el.dataset.start;
  const endRaw = el.dataset.end;
  if (!name || !startRaw || !endRaw) return null;

  const start = Number(startRaw);
  const end = Number(endRaw);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return null;

  const position = lineColumnAtOffset(lineStarts, start);
  return {
    name,
    start,
    end,
    line: position.line,
    column: position.column,
  };
}

function sameToken(a: SymbolToken | null, b: SymbolToken): boolean {
  return !!a && a.start === b.start && a.end === b.end && a.name === b.name;
}

function prioritizeDefinitions(definitions: SymbolResult[], currentFile: string, currentLine: number): SymbolResult[] {
  const withScore = definitions.map((definition, index) => ({
    definition,
    index,
    score: definitionScore(definition, currentFile, currentLine),
  }));
  withScore.sort((left, right) => right.score - left.score || left.index - right.index);
  return withScore.map((item) => item.definition);
}

function definitionScore(definition: SymbolResult, currentFile: string, currentLine: number): number {
  let score = 0;
  if (currentFile && definition.file === currentFile) score += 4;
  if (
    currentFile
    && definition.file === currentFile
    && currentLine > 0
    && typeof definition.start_line === 'number'
    && typeof definition.end_line === 'number'
    && currentLine >= definition.start_line
    && currentLine <= definition.end_line
  ) {
    score += 6;
  }
  return score;
}

function prioritizeByCurrentFile<T extends { file: string }>(items: T[], currentFile: string): T[] {
  if (!currentFile) return items;
  const local: T[] = [];
  const remote: T[] = [];
  for (const item of items) {
    if (item.file === currentFile) local.push(item);
    else remote.push(item);
  }
  return [...local, ...remote];
}

function buildBlobHref(owner: string | undefined, repo: string | undefined, gitRef: string | undefined, file: string, line?: number): string {
  if (!owner || !repo || !gitRef || !file) return '#';
  const base = `/${owner}/${repo}/blob/${gitRef}/${file}`;
  return line && line > 0 ? `${base}#L${line}` : base;
}

function formatLineRange(start?: number, end?: number): string {
  if (!start || start <= 0) return 'line ?';
  if (!end || end <= start) return `L${start}`;
  return `L${start}-${end}`;
}

function formatReferenceLocation(reference: ReferenceResult): string {
  const line = reference.start_line || reference.line;
  const endLine = reference.end_line;
  const column = reference.start_column;
  const endColumn = reference.end_column;

  let text = line && line > 0 ? `L${line}` : 'line ?';
  if (line && endLine && endLine > line) text += `-${endLine}`;
  if (column && column > 0) {
    text += `, col ${column}`;
    if (endColumn && endColumn > column) text += `-${endColumn}`;
  }
  return text;
}

function shortHash(hash: string): string {
  if (!hash) return '';
  return hash.length > 12 ? hash.slice(0, 12) : hash;
}

function formatTimestamp(ts: number): string {
  if (!ts) return 'unknown time';
  return new Date(ts * 1000).toLocaleString();
}

function buildLineStarts(text: string): number[] {
  const starts = [0];
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) === 10) {
      starts.push(i + 1);
    }
  }
  return starts;
}

function lineColumnAtOffset(lineStarts: number[], offset: number): { line: number; column: number } {
  if (lineStarts.length === 0) return { line: 1, column: offset + 1 };

  const clampedOffset = Math.max(0, offset);
  let low = 0;
  let high = lineStarts.length - 1;

  while (low <= high) {
    const mid = (low + high) >> 1;
    if (lineStarts[mid] <= clampedOffset) {
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }

  const lineIndex = Math.max(0, high);
  const lineStart = lineStarts[lineIndex] || 0;
  return {
    line: lineIndex + 1,
    column: clampedOffset - lineStart + 1,
  };
}
