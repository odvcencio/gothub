import type { ComponentChildren, JSX } from 'preact';
import { useEffect, useMemo, useState } from 'preact/hooks';
import { CodeViewer } from './CodeViewer';

interface Props {
  filename: string;
  source: string;
  owner?: string;
  repo?: string;
  gitRef?: string;
  path?: string;
}

type ViewMode = 'preview' | 'raw';
type ListType = 'ordered' | 'unordered';
type InlineMatchType = 'code' | 'link' | 'strong' | 'em' | 'strike';

interface InlineMatch {
  type: InlineMatchType;
  index: number;
  raw: string;
  content: string;
  href?: string;
}

interface FenceStart {
  markerChar: '`' | '~';
  markerLength: number;
  language: string;
}

const headingPattern = /^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/;
const orderedListPattern = /^ {0,3}(\d+)[.)]\s+(.+)$/;
const unorderedListPattern = /^ {0,3}[-+*]\s+(.+)$/;
const quotePattern = /^ {0,3}>\s?(.*)$/;
const horizontalRulePattern = /^ {0,3}((\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$/;
const continuationLinePattern = /^( {2,}|\t+)\S/;
const safeSchemePattern = /^([A-Za-z][A-Za-z0-9+.-]*):/;

const previewShellStyle: JSX.CSSProperties = {
  border: '1px solid #30363d',
  borderRadius: '6px',
  overflow: 'hidden',
  background: '#0d1117',
};

const toolbarStyle: JSX.CSSProperties = {
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
  padding: '10px 12px',
  borderBottom: '1px solid #30363d',
  background: '#161b22',
};

const articleStyle: JSX.CSSProperties = {
  padding: '16px',
  color: '#c9d1d9',
  fontSize: '14px',
  lineHeight: 1.6,
  overflowWrap: 'anywhere',
};

const paragraphStyle: JSX.CSSProperties = {
  margin: '0 0 14px',
};

const horizontalRuleStyle: JSX.CSSProperties = {
  border: 0,
  borderTop: '1px solid #30363d',
  margin: '16px 0',
};

const blockquoteStyle: JSX.CSSProperties = {
  margin: '0 0 14px',
  padding: '2px 0 2px 12px',
  borderLeft: '3px solid #30363d',
  color: '#adbac7',
};

const listStyle: JSX.CSSProperties = {
  margin: '0 0 14px 22px',
  padding: 0,
  display: 'grid',
  gap: '6px',
};

const listItemStyle: JSX.CSSProperties = {
  margin: 0,
};

const headingStyles: Record<number, JSX.CSSProperties> = {
  1: {
    margin: '0 0 14px',
    fontSize: '1.85rem',
    lineHeight: 1.2,
    color: '#f0f6fc',
    borderBottom: '1px solid #30363d',
    paddingBottom: '8px',
  },
  2: {
    margin: '0 0 12px',
    fontSize: '1.5rem',
    lineHeight: 1.25,
    color: '#f0f6fc',
    borderBottom: '1px solid #30363d',
    paddingBottom: '6px',
  },
  3: {
    margin: '0 0 10px',
    fontSize: '1.25rem',
    lineHeight: 1.3,
    color: '#f0f6fc',
  },
  4: {
    margin: '0 0 8px',
    fontSize: '1.1rem',
    lineHeight: 1.35,
    color: '#f0f6fc',
  },
  5: {
    margin: '0 0 8px',
    fontSize: '1rem',
    lineHeight: 1.4,
    color: '#f0f6fc',
  },
  6: {
    margin: '0 0 8px',
    fontSize: '0.95rem',
    lineHeight: 1.4,
    color: '#8b949e',
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
  },
};

const inlineCodeStyle: JSX.CSSProperties = {
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
  background: 'rgba(110, 118, 129, 0.2)',
  border: '1px solid rgba(110, 118, 129, 0.4)',
  borderRadius: '4px',
  padding: '0 4px',
  fontSize: '0.92em',
};

const codeBlockStyle: JSX.CSSProperties = {
  margin: '0 0 14px',
  border: '1px solid #30363d',
  borderRadius: '6px',
  overflow: 'hidden',
  background: '#0b1220',
};

const codeBlockCaptionStyle: JSX.CSSProperties = {
  margin: 0,
  padding: '5px 10px',
  borderBottom: '1px solid #30363d',
  color: '#8b949e',
  fontSize: '11px',
  textTransform: 'uppercase',
  letterSpacing: '0.03em',
};

const codePreStyle: JSX.CSSProperties = {
  margin: 0,
  padding: '12px 14px',
  overflowX: 'auto',
  fontSize: '13px',
  lineHeight: 1.5,
  color: '#c9d1d9',
};

const linkStyle: JSX.CSSProperties = {
  color: '#58a6ff',
  textDecoration: 'underline',
  textUnderlineOffset: '2px',
};

export function MarkdownViewer({ filename, source, owner, repo, gitRef, path }: Props) {
  const [mode, setMode] = useState<ViewMode>('preview');
  const renderedPreview = useMemo(() => renderMarkdownBlocks(source, 'md'), [source]);

  useEffect(() => {
    setMode('preview');
  }, [filename, path]);

  return (
    <div style={{ display: 'grid', gap: '10px' }}>
      <div style={previewShellStyle}>
        <div style={toolbarStyle}>
          <strong style={{ color: '#f0f6fc', fontSize: '13px' }}>Markdown</strong>
          <div style={{ display: 'flex', gap: '6px' }}>
            <ModeButton label="Preview" active={mode === 'preview'} onClick={() => setMode('preview')} />
            <ModeButton label="Raw" active={mode === 'raw'} onClick={() => setMode('raw')} />
          </div>
        </div>

        {mode === 'preview' ? (
          <article style={articleStyle}>
            {renderedPreview.length > 0 ? (
              renderedPreview
            ) : (
              <p style={{ margin: 0, color: '#8b949e' }}>Empty markdown file.</p>
            )}
          </article>
        ) : (
          <div style={{ padding: '12px' }}>
            <CodeViewer
              filename={filename}
              source={source}
              owner={owner}
              repo={repo}
              gitRef={gitRef}
              path={path}
            />
          </div>
        )}
      </div>
    </div>
  );
}

function ModeButton({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      style={{
        background: active ? '#1f6feb' : '#21262d',
        color: active ? '#ffffff' : '#c9d1d9',
        border: '1px solid #30363d',
        borderRadius: '4px',
        fontSize: '12px',
        padding: '4px 10px',
        cursor: 'pointer',
      }}
    >
      {label}
    </button>
  );
}

function renderMarkdownBlocks(source: string, keyPrefix: string): JSX.Element[] {
  const normalized = source.replace(/\r\n?/g, '\n');
  const lines = normalized.split('\n');
  const nodes: JSX.Element[] = [];

  let lineIndex = 0;
  let blockIndex = 0;

  while (lineIndex < lines.length) {
    const line = lines[lineIndex] || '';
    if (line.trim() === '') {
      lineIndex += 1;
      continue;
    }

    const fence = parseFenceStart(line);
    if (fence) {
      lineIndex += 1;
      const codeLines: string[] = [];
      while (lineIndex < lines.length && !isFenceEnd(lines[lineIndex] || '', fence)) {
        codeLines.push(lines[lineIndex] || '');
        lineIndex += 1;
      }
      if (lineIndex < lines.length) lineIndex += 1;

      nodes.push(
        <figure key={`${keyPrefix}-code-${blockIndex}`} style={codeBlockStyle}>
          {fence.language && <figcaption style={codeBlockCaptionStyle}>{fence.language}</figcaption>}
          <pre style={codePreStyle}>
            <code>{codeLines.join('\n')}</code>
          </pre>
        </figure>,
      );
      blockIndex += 1;
      continue;
    }

    const heading = headingPattern.exec(line);
    if (heading) {
      const level = Math.min(6, heading[1].length);
      const headingText = heading[2] || '';
      nodes.push(renderHeading(level, renderInline(headingText, `${keyPrefix}-h-${blockIndex}`), `${keyPrefix}-h-${blockIndex}`));
      blockIndex += 1;
      lineIndex += 1;
      continue;
    }

    if (horizontalRulePattern.test(line)) {
      nodes.push(<hr key={`${keyPrefix}-hr-${blockIndex}`} style={horizontalRuleStyle} />);
      blockIndex += 1;
      lineIndex += 1;
      continue;
    }

    if (quotePattern.test(line)) {
      const quoteLines: string[] = [];
      while (lineIndex < lines.length) {
        const quoteMatch = quotePattern.exec(lines[lineIndex] || '');
        if (!quoteMatch) break;
        quoteLines.push(quoteMatch[1] || '');
        lineIndex += 1;
      }

      nodes.push(
        <blockquote key={`${keyPrefix}-quote-${blockIndex}`} style={blockquoteStyle}>
          {renderMarkdownBlocks(quoteLines.join('\n'), `${keyPrefix}-quote-${blockIndex}`)}
        </blockquote>,
      );
      blockIndex += 1;
      continue;
    }

    const orderedList = orderedListPattern.exec(line);
    const unorderedList = unorderedListPattern.exec(line);
    if (orderedList || unorderedList) {
      const listType: ListType = orderedList ? 'ordered' : 'unordered';
      const { node, nextIndex } = consumeList(lines, lineIndex, listType, `${keyPrefix}-list-${blockIndex}`);
      nodes.push(node);
      blockIndex += 1;
      lineIndex = nextIndex;
      continue;
    }

    const paragraphLines: string[] = [];
    while (lineIndex < lines.length) {
      const nextLine = lines[lineIndex] || '';
      if (nextLine.trim() === '' || isBlockStart(nextLine)) break;
      paragraphLines.push(nextLine.trim());
      lineIndex += 1;
    }

    const paragraphText = paragraphLines.join(' ').replace(/\s+/g, ' ').trim();
    if (paragraphText) {
      nodes.push(
        <p key={`${keyPrefix}-p-${blockIndex}`} style={paragraphStyle}>
          {renderInline(paragraphText, `${keyPrefix}-p-${blockIndex}`)}
        </p>,
      );
      blockIndex += 1;
    } else {
      lineIndex += 1;
    }
  }

  return nodes;
}

function consumeList(
  lines: string[],
  startIndex: number,
  listType: ListType,
  keyPrefix: string,
): { node: JSX.Element; nextIndex: number } {
  const items: JSX.Element[] = [];
  const itemPattern = listType === 'ordered' ? orderedListPattern : unorderedListPattern;

  let lineIndex = startIndex;
  let itemIndex = 0;
  let orderedStart = 1;

  while (lineIndex < lines.length) {
    const match = itemPattern.exec(lines[lineIndex] || '');
    if (!match) break;

    const parts: string[] = [];
    if (listType === 'ordered') {
      if (itemIndex === 0) orderedStart = Number(match[1]) || 1;
      parts.push(match[2] || '');
    } else {
      parts.push(match[1] || '');
    }
    lineIndex += 1;

    while (lineIndex < lines.length) {
      const nextLine = lines[lineIndex] || '';
      if (nextLine.trim() === '') break;
      if (itemPattern.test(nextLine)) break;
      if (isBlockStart(nextLine) && !continuationLinePattern.test(nextLine)) break;
      if (continuationLinePattern.test(nextLine)) {
        parts.push(nextLine.trim());
        lineIndex += 1;
        continue;
      }
      break;
    }

    const text = parts.join(' ').replace(/\s+/g, ' ').trim();
    items.push(
      <li key={`${keyPrefix}-item-${itemIndex}`} style={listItemStyle}>
        {renderInline(text, `${keyPrefix}-item-${itemIndex}`)}
      </li>,
    );
    itemIndex += 1;
  }

  if (listType === 'ordered') {
    return {
      node: (
        <ol key={keyPrefix} style={listStyle} start={orderedStart > 1 ? orderedStart : undefined}>
          {items}
        </ol>
      ),
      nextIndex: lineIndex,
    };
  }

  return {
    node: (
      <ul key={keyPrefix} style={listStyle}>
        {items}
      </ul>
    ),
    nextIndex: lineIndex,
  };
}

function renderHeading(level: number, children: ComponentChildren, key: string): JSX.Element {
  const safeLevel = Math.min(6, Math.max(1, level));
  const style = headingStyles[safeLevel] || headingStyles[3];

  if (safeLevel === 1) return <h1 key={key} style={style}>{children}</h1>;
  if (safeLevel === 2) return <h2 key={key} style={style}>{children}</h2>;
  if (safeLevel === 3) return <h3 key={key} style={style}>{children}</h3>;
  if (safeLevel === 4) return <h4 key={key} style={style}>{children}</h4>;
  if (safeLevel === 5) return <h5 key={key} style={style}>{children}</h5>;
  return <h6 key={key} style={style}>{children}</h6>;
}

function renderInline(text: string, keyPrefix: string, depth = 0): ComponentChildren[] {
  if (!text) return [];
  if (depth > 8) return [text];

  const nodes: ComponentChildren[] = [];
  let remaining = text;
  let chunkIndex = 0;

  while (remaining.length > 0) {
    const next = findFirstInlineMatch(remaining);
    if (!next) {
      nodes.push(remaining);
      break;
    }

    if (next.index > 0) {
      nodes.push(remaining.slice(0, next.index));
    }

    const key = `${keyPrefix}-inline-${chunkIndex}`;
    if (next.type === 'code') {
      nodes.push(<code key={key} style={inlineCodeStyle}>{next.content}</code>);
    } else if (next.type === 'link') {
      const href = sanitizeHref(next.href || '');
      const linkChildren = renderInline(next.content, `${key}-link`, depth + 1);
      if (href) {
        const external = /^https?:\/\//i.test(href);
        nodes.push(
          <a
            key={key}
            href={href}
            style={linkStyle}
            target={external ? '_blank' : undefined}
            rel={external ? 'noopener noreferrer' : undefined}
          >
            {linkChildren}
          </a>,
        );
      } else {
        nodes.push(
          <span key={key} style={{ color: '#8b949e' }}>
            {linkChildren}
          </span>,
        );
      }
    } else if (next.type === 'strong') {
      nodes.push(<strong key={key}>{renderInline(next.content, `${key}-strong`, depth + 1)}</strong>);
    } else if (next.type === 'em') {
      nodes.push(<em key={key}>{renderInline(next.content, `${key}-em`, depth + 1)}</em>);
    } else if (next.type === 'strike') {
      nodes.push(<del key={key}>{renderInline(next.content, `${key}-strike`, depth + 1)}</del>);
    }

    if (next.raw.length === 0) {
      nodes.push(remaining);
      break;
    }

    remaining = remaining.slice(next.index + next.raw.length);
    chunkIndex += 1;
  }

  return nodes;
}

function findFirstInlineMatch(text: string): InlineMatch | null {
  const matches: InlineMatch[] = [];

  const code = /`([^`\n]+)`/.exec(text);
  if (code) {
    matches.push({
      type: 'code',
      index: code.index,
      raw: code[0],
      content: code[1] || '',
    });
  }

  const link = /\[([^\]]+)\]\(([^)]+)\)/.exec(text);
  if (link) {
    matches.push({
      type: 'link',
      index: link.index,
      raw: link[0],
      content: link[1] || '',
      href: link[2] || '',
    });
  }

  const strong = /\*\*([^*]+)\*\*|__([^_]+)__/.exec(text);
  if (strong) {
    matches.push({
      type: 'strong',
      index: strong.index,
      raw: strong[0],
      content: strong[1] || strong[2] || '',
    });
  }

  const emphasis = /\*([^*\n]+)\*|_([^_\n]+)_/.exec(text);
  if (emphasis) {
    matches.push({
      type: 'em',
      index: emphasis.index,
      raw: emphasis[0],
      content: emphasis[1] || emphasis[2] || '',
    });
  }

  const strike = /~~([^~]+)~~/.exec(text);
  if (strike) {
    matches.push({
      type: 'strike',
      index: strike.index,
      raw: strike[0],
      content: strike[1] || '',
    });
  }

  if (matches.length === 0) return null;

  matches.sort((left, right) => left.index - right.index || inlineTypePriority(left.type) - inlineTypePriority(right.type));
  return matches[0] || null;
}

function inlineTypePriority(type: InlineMatchType): number {
  if (type === 'code') return 0;
  if (type === 'link') return 1;
  if (type === 'strong') return 2;
  if (type === 'em') return 3;
  return 4;
}

function sanitizeHref(rawHref: string): string | null {
  const trimmed = rawHref.trim();
  if (!trimmed) return null;

  const href = trimmed.startsWith('<') && trimmed.endsWith('>')
    ? trimmed.slice(1, -1).trim()
    : trimmed;
  if (!href) return null;

  if (href.startsWith('#')) return href;
  if (/^(\/|\.\/|\.\.\/)/.test(href)) return href;

  const scheme = safeSchemePattern.exec(href);
  if (!scheme) return href;

  const protocol = `${(scheme[1] || '').toLowerCase()}:`;
  if (protocol === 'http:' || protocol === 'https:' || protocol === 'mailto:') return href;
  return null;
}

function parseFenceStart(line: string): FenceStart | null {
  const match = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
  if (!match) return null;

  const marker = match[1] || '';
  const markerChar = marker.startsWith('~') ? '~' : '`';
  const markerLength = marker.length;
  const language = ((match[2] || '').trim().split(/\s+/)[0] || '').toLowerCase();

  return {
    markerChar,
    markerLength,
    language,
  };
}

function isFenceEnd(line: string, fence: FenceStart): boolean {
  const trimmed = line.trim();
  if (!trimmed) return false;
  if (trimmed[0] !== fence.markerChar) return false;

  let count = 0;
  while (count < trimmed.length && trimmed[count] === fence.markerChar) count += 1;
  if (count < fence.markerLength) return false;

  for (let i = count; i < trimmed.length; i += 1) {
    if (!/\s/.test(trimmed[i] || '')) return false;
  }
  return true;
}

function isBlockStart(line: string): boolean {
  return !!(
    line.trim() === ''
    || parseFenceStart(line)
    || headingPattern.test(line)
    || orderedListPattern.test(line)
    || unorderedListPattern.test(line)
    || quotePattern.test(line)
    || horizontalRulePattern.test(line)
  );
}
