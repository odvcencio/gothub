import { useState, useEffect } from 'preact/hooks';
import { getRepo, listTree } from '../api/client';
import { FileTree } from '../components/FileTree';

interface Props {
  owner?: string;
  repo?: string;
}

export function RepoView({ owner, repo }: Props) {
  const [repoInfo, setRepoInfo] = useState<any>(null);
  const [entries, setEntries] = useState<any[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo) return;
    getRepo(owner, repo).then(setRepoInfo).catch(e => setError(e.message));
  }, [owner, repo]);

  useEffect(() => {
    if (!repoInfo) return;
    const ref = repoInfo.default_branch || 'main';
    listTree(owner!, repo!, ref).then(setEntries).catch(() => {});
  }, [repoInfo]);

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  if (!repoInfo) return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;

  const ref = repoInfo.default_branch || 'main';

  return (
    <div>
      <div style={{ marginBottom: '20px' }}>
        <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '4px' }}>
          <a href={`/${owner}`} style={{ color: '#58a6ff' }}>{owner}</a>
          <span style={{ color: '#8b949e' }}> / </span>
          <a href={`/${owner}/${repo}`} style={{ color: '#58a6ff', fontWeight: 'bold' }}>{repo}</a>
          {repoInfo.is_private && <span style={{ color: '#8b949e', fontSize: '12px', fontWeight: 'normal', marginLeft: '8px', border: '1px solid #30363d', padding: '2px 8px', borderRadius: '12px' }}>Private</span>}
        </h1>
        {repoInfo.description && <p style={{ color: '#8b949e', fontSize: '14px' }}>{repoInfo.description}</p>}
      </div>

      <div style={{ display: 'flex', gap: '12px', marginBottom: '20px' }}>
        <a href={`/${owner}/${repo}/tree/${ref}`} style={tabStyle}>Code</a>
        <a href={`/${owner}/${repo}/commits/${ref}`} style={tabStyle}>Commits</a>
        <a href={`/${owner}/${repo}/pulls`} style={tabStyle}>Pull requests</a>
      </div>

      {entries.length > 0 ? (
        <FileTree entries={entries} owner={owner!} repo={repo!} ref={ref} currentPath="" />
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '40px', textAlign: 'center' }}>
          <p style={{ color: '#8b949e', marginBottom: '16px' }}>This repository is empty</p>
          <div style={{ background: '#161b22', padding: '16px', borderRadius: '6px', fontFamily: 'monospace', fontSize: '13px', color: '#c9d1d9', textAlign: 'left', display: 'inline-block' }}>
            <div>got init</div>
            <div>got add .</div>
            <div>got commit -m "initial commit"</div>
            <div>got remote add origin {location.origin}/got/{owner}/{repo}</div>
            <div>got push origin main</div>
          </div>
        </div>
      )}
    </div>
  );
}

const tabStyle = {
  color: '#c9d1d9',
  padding: '8px 16px',
  borderRadius: '6px',
  fontSize: '14px',
  border: '1px solid #30363d',
};
