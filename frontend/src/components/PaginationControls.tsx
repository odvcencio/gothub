import { JSX } from 'preact';

interface Props {
  page: number;
  hasNext: boolean;
  onPageChange: (page: number) => void;
  disabled?: boolean;
  style?: JSX.CSSProperties;
}

export function PaginationControls({ page, hasNext, onPageChange, disabled = false, style }: Props) {
  const previousDisabled = disabled || page <= 1;
  const nextDisabled = disabled || !hasNext;

  return (
    <div style={{ ...containerStyle, ...style }}>
      <button
        onClick={() => onPageChange(Math.max(1, page - 1))}
        disabled={previousDisabled}
        style={pagerButtonStyle(previousDisabled)}
      >
        Previous
      </button>
      <span style={pageLabelStyle}>Page {page}</span>
      <button
        onClick={() => onPageChange(page + 1)}
        disabled={nextDisabled}
        style={pagerButtonStyle(nextDisabled)}
      >
        Next
      </button>
    </div>
  );
}

const containerStyle: JSX.CSSProperties = {
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
  marginTop: '12px',
  gap: '12px',
};

const pageLabelStyle: JSX.CSSProperties = {
  color: '#8b949e',
  fontSize: '13px',
};

function pagerButtonStyle(disabled: boolean): JSX.CSSProperties {
  return {
    background: disabled ? '#161b22' : '#21262d',
    color: disabled ? '#6e7681' : '#c9d1d9',
    border: '1px solid #30363d',
    padding: '6px 12px',
    borderRadius: '6px',
    cursor: disabled ? 'not-allowed' : 'pointer',
    fontSize: '13px',
  };
}
