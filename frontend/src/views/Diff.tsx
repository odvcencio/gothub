import { useState, useEffect } from 'preact/hooks';
import { getDiff } from '../api/client';
import { EntityDiff } from '../components/EntityDiff';

interface Props {
  owner?: string;
  repo?: string;
  spec?: string;
}

export function DiffView({ owner, repo, spec }: Props) {
  const [diff, setDiff] = useState<any>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo || !spec) return;
    getDiff(owner, repo, spec).then(setDiff).catch(e => setError(e.message));
  }, [owner, repo, spec]);

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;

  const parts = spec?.split('...') || [];

  return (
    <div>
      <h2 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '16px' }}>
        Comparing{' '}
        <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px', fontSize: '16px' }}>{parts[0]}</code>
        {' '}...{' '}
        <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px', fontSize: '16px' }}>{parts[1]}</code>
      </h2>

      {diff ? (
        <EntityDiff files={diff.files || []} />
      ) : (
        <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>
      )}
    </div>
  );
}
