import { useState, useEffect } from 'preact/hooks';
import { forkRepo, getRepo, getRepoStars, getToken, listRepoForks, listTree, starRepo, unstarRepo } from '../api/client';
import { FileTree } from '../components/FileTree';

interface Props {
  owner?: string;
  repo?: string;
}

export function RepoView({ owner, repo }: Props) {
  const [repoInfo, setRepoInfo] = useState<any>(null);
  const [stars, setStars] = useState<{ count: number; starred: boolean } | null>(null);
  const [forks, setForks] = useState<any[]>([]);
  const [starring, setStarring] = useState(false);
  const [forking, setForking] = useState(false);
  const [entries, setEntries] = useState<any[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!owner || !repo) return;
    getRepo(owner, repo).then(setRepoInfo).catch(e => setError(e.message));
    getRepoStars(owner, repo).then(setStars).catch(e => setError(e.message || 'failed to load stars'));
  }, [owner, repo]);

  useEffect(() => {
    if (!owner || !repo || !repoInfo) return;
    const ref = repoInfo.default_branch || 'main';
    listTree(owner, repo, ref).then(setEntries).catch(e => setError(e.message || 'failed to load repository tree'));
  }, [owner, repo, repoInfo]);

  useEffect(() => {
    if (!owner || !repo) return;
    listRepoForks(owner, repo).then(setForks).catch(e => setError(e.message || 'failed to load forks'));
  }, [owner, repo]);

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  if (!repoInfo) return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;

  const ref = repoInfo.default_branch || 'main';
  const loggedIn = !!getToken();

  const onToggleStar = async () => {
    if (!owner || !repo || !loggedIn || !stars || starring) return;
    setStarring(true);
    try {
      const next = stars.starred ? await unstarRepo(owner, repo) : await starRepo(owner, repo);
      setStars(next);
    } catch (e: any) {
      setError(e.message || 'failed to update star');
    }
    setStarring(false);
  };

  const onFork = async () => {
    if (!owner || !repo || !loggedIn || forking) return;
    const suggested = repoInfo?.parent_repo_id ? `${repo}-fork` : repo;
    const entered = window.prompt('Fork repository name', suggested);
    if (entered === null) return;
    const name = entered.trim();
    setForking(true);
    try {
      const created = await forkRepo(owner, repo, name || undefined);
      const targetOwner = created.owner_name || owner;
      location.href = `/${targetOwner}/${created.name}`;
    } catch (e: any) {
      setError(e.message || 'failed to fork repository');
    } finally {
      setForking(false);
    }
  };

  return (
    <div>
      <div style={{ marginBottom: '20px' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', flexWrap: 'wrap' }}>
          <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '4px' }}>
            <a href={`/${owner}`} style={{ color: '#58a6ff' }}>{owner}</a>
            <span style={{ color: '#8b949e' }}> / </span>
            <a href={`/${owner}/${repo}`} style={{ color: '#58a6ff', fontWeight: 'bold' }}>{repo}</a>
            {repoInfo.is_private && <span style={{ color: '#8b949e', fontSize: '12px', fontWeight: 'normal', marginLeft: '8px', border: '1px solid #30363d', padding: '2px 8px', borderRadius: '12px' }}>Private</span>}
          </h1>
          <div style={{ display: 'flex', gap: '8px' }}>
            <button
              onClick={onToggleStar}
              disabled={!loggedIn || !stars || starring}
              style={{
                border: '1px solid #30363d',
                background: stars?.starred ? '#1f6feb' : '#161b22',
                color: stars?.starred ? '#fff' : '#c9d1d9',
                padding: '6px 12px',
                borderRadius: '6px',
                cursor: !loggedIn || starring ? 'not-allowed' : 'pointer',
                fontSize: '13px',
              }}
              title={loggedIn ? '' : 'Sign in to star this repository'}
            >
              {stars?.starred ? 'Starred' : 'Star'} {stars ? `(${stars.count})` : ''}
            </button>
            <button
              onClick={onFork}
              disabled={!loggedIn || forking}
              style={{
                border: '1px solid #30363d',
                background: '#161b22',
                color: '#c9d1d9',
                padding: '6px 12px',
                borderRadius: '6px',
                cursor: !loggedIn || forking ? 'not-allowed' : 'pointer',
                fontSize: '13px',
              }}
              title={loggedIn ? '' : 'Sign in to fork this repository'}
            >
              {forking ? 'Forking...' : `Fork${forks.length > 0 ? ` (${forks.length})` : ''}`}
            </button>
          </div>
        </div>
        {repoInfo.description && <p style={{ color: '#8b949e', fontSize: '14px' }}>{repoInfo.description}</p>}
        {repoInfo.parent_owner && repoInfo.parent_name && (
          <p style={{ color: '#8b949e', fontSize: '13px' }}>
            Forked from <a href={`/${repoInfo.parent_owner}/${repoInfo.parent_name}`} style={{ color: '#58a6ff' }}>{repoInfo.parent_owner}/{repoInfo.parent_name}</a>
          </p>
        )}
      </div>

      <div style={{ display: 'flex', gap: '12px', marginBottom: '20px', flexWrap: 'wrap' }}>
        <a href={`/${owner}/${repo}/tree/${ref}`} style={tabStyle}>Code</a>
        <a href={`/${owner}/${repo}/commits/${ref}`} style={tabStyle}>Commits</a>
        <a href={`/${owner}/${repo}/pulls`} style={tabStyle}>Pull requests</a>
        <a href={`/${owner}/${repo}/issues`} style={tabStyle}>Issues</a>
        <a href={`/${owner}/${repo}/symbols/${ref}`} style={tabStyle}>Symbols</a>
        <a href={`/${owner}/${repo}/entity-history/${ref}`} style={tabStyle}>Entity History</a>
        {loggedIn && <a href={`/${owner}/${repo}/settings`} style={tabStyle}>Settings</a>}
      </div>

      {entries.length > 0 ? (
        owner && repo
          ? <FileTree entries={entries} owner={owner} repo={repo} ref={ref} currentPath="" />
          : <div style={{ color: '#f85149', padding: '20px' }}>Missing repository context</div>
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
      {forks.length > 0 && (
        <div style={{ marginTop: '20px', border: '1px solid #30363d', borderRadius: '6px', padding: '12px 16px' }}>
          <div style={{ color: '#f0f6fc', fontSize: '14px', marginBottom: '8px' }}>Forks</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px' }}>
            {forks.slice(0, 12).map((f) => (
              <a key={f.id} href={`/${f.owner_name}/${f.name}`} style={{ color: '#58a6ff', fontSize: '13px' }}>
                {f.owner_name}/{f.name}
              </a>
            ))}
            {forks.length > 12 && (
              <span style={{ color: '#8b949e', fontSize: '13px' }}>+{forks.length - 12} more</span>
            )}
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
