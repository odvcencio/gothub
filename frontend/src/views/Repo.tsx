import { useEffect, useMemo, useState } from 'preact/hooks';
import {
  forkRepo,
  getRepo,
  getRepoStars,
  getToken,
  listRepoForks,
  listTree,
  starRepo,
  unstarRepo,
  type Repository,
  type RepoStars,
  type TreeEntry,
} from '../api/client';
import { FileTree } from '../components/FileTree';

interface Props {
  owner?: string;
  repo?: string;
}

type RepoTarget = {
  owner: string;
  name: string;
};

export function RepoView({ owner, repo }: Props) {
  const [repoInfo, setRepoInfo] = useState<Repository | null>(null);
  const [repoLoading, setRepoLoading] = useState(false);
  const [repoError, setRepoError] = useState('');

  const [stars, setStars] = useState<RepoStars | null>(null);
  const [starsLoading, setStarsLoading] = useState(false);
  const [starsError, setStarsError] = useState('');
  const [starring, setStarring] = useState(false);

  const [forks, setForks] = useState<Repository[]>([]);
  const [forksLoading, setForksLoading] = useState(false);
  const [forksError, setForksError] = useState('');
  const [forkPanelOpen, setForkPanelOpen] = useState(false);
  const [forkName, setForkName] = useState('');
  const [forking, setForking] = useState(false);
  const [forkError, setForkError] = useState('');
  const [forkNotice, setForkNotice] = useState('');

  const [entries, setEntries] = useState<TreeEntry[]>([]);
  const [treeLoading, setTreeLoading] = useState(false);
  const [treeError, setTreeError] = useState('');

  const [upstream, setUpstream] = useState<RepoTarget | null>(null);
  const [upstreamLoading, setUpstreamLoading] = useState(false);

  const loggedIn = !!getToken();

  const parentRepo = useMemo<RepoTarget | null>(() => {
    if (!repoInfo?.parent_owner || !repoInfo?.parent_name) return null;
    return { owner: repoInfo.parent_owner, name: repoInfo.parent_name };
  }, [repoInfo?.parent_owner, repoInfo?.parent_name]);

  const defaultRef = repoInfo?.default_branch || 'main';

  const loadForks = () => {
    if (!owner || !repo) return;
    setForksLoading(true);
    setForksError('');
    listRepoForks(owner, repo)
      .then((items) => setForks(items || []))
      .catch((e: any) => setForksError(e?.message || 'failed to load forks'))
      .finally(() => setForksLoading(false));
  };

  const loadTree = (refName: string) => {
    if (!owner || !repo) return;
    setTreeLoading(true);
    setTreeError('');
    listTree(owner, repo, refName)
      .then((items) => setEntries(items || []))
      .catch((e: any) => {
        setEntries([]);
        setTreeError(e?.message || 'failed to load repository tree');
      })
      .finally(() => setTreeLoading(false));
  };

  useEffect(() => {
    if (!owner || !repo) return;

    let cancelled = false;
    setRepoLoading(true);
    setRepoError('');
    setRepoInfo(null);

    getRepo(owner, repo)
      .then((info) => {
        if (cancelled) return;
        if (!info) {
          setRepoError('Repository data unavailable');
          return;
        }
        setRepoInfo(info);
      })
      .catch((e: any) => {
        if (cancelled) return;
        setRepoError(e?.message || 'failed to load repository');
      })
      .finally(() => {
        if (cancelled) return;
        setRepoLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [owner, repo]);

  useEffect(() => {
    if (!owner || !repo) return;

    let cancelled = false;
    setStarsLoading(true);
    setStarsError('');

    getRepoStars(owner, repo)
      .then((next) => {
        if (cancelled) return;
        setStars(next);
      })
      .catch((e: any) => {
        if (cancelled) return;
        setStars(null);
        setStarsError(e?.message || 'failed to load stars');
      })
      .finally(() => {
        if (cancelled) return;
        setStarsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [owner, repo]);

  useEffect(() => {
    if (!owner || !repo) return;
    loadForks();
  }, [owner, repo]);

  useEffect(() => {
    if (!owner || !repo || !repoInfo) return;
    loadTree(repoInfo.default_branch || 'main');
  }, [owner, repo, repoInfo?.id, repoInfo?.default_branch]);

  useEffect(() => {
    if (!parentRepo) {
      setUpstream(null);
      setUpstreamLoading(false);
      return;
    }

    let cancelled = false;
    setUpstream(parentRepo);
    setUpstreamLoading(true);

    const resolveUpstream = async () => {
      let current: RepoTarget = { ...parentRepo };

      for (let depth = 0; depth < 6; depth++) {
        const currentRepo = await getRepo(current.owner, current.name);
        if (!currentRepo.parent_owner || !currentRepo.parent_name) {
          break;
        }
        current = {
          owner: currentRepo.parent_owner,
          name: currentRepo.parent_name,
        };
      }

      if (!cancelled) {
        setUpstream(current);
      }
    };

    resolveUpstream()
      .catch(() => {
        if (cancelled) return;
        setUpstream(parentRepo);
      })
      .finally(() => {
        if (cancelled) return;
        setUpstreamLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [parentRepo?.owner, parentRepo?.name]);

  if (!owner || !repo) {
    return <div style={{ color: '#f85149', padding: '20px' }}>Missing repository context</div>;
  }

  if (repoError) {
    return <div style={{ color: '#f85149', padding: '20px' }}>{repoError}</div>;
  }

  if (repoLoading || !repoInfo) {
    return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;
  }

  const showUpstream =
    !!parentRepo &&
    !!upstream &&
    (upstream.owner !== parentRepo.owner || upstream.name !== parentRepo.name);

  const starLabel = stars
    ? `${stars.starred ? 'Starred' : 'Star'} (${stars.count})`
    : 'Star';

  const forkCountLabel = forksLoading
    ? ' (...)'
    : forks.length > 0
      ? ` (${forks.length})`
      : '';

  const openForkPanel = () => {
    if (!owner || !repo || !loggedIn || forking) return;

    if (forkPanelOpen) {
      setForkPanelOpen(false);
      setForkError('');
      return;
    }

    const suggested = repoInfo.parent_repo_id ? `${repo}-fork` : repo;
    setForkName(suggested);
    setForkError('');
    setForkNotice('');
    setForkPanelOpen(true);
  };

  const onToggleStar = async () => {
    if (!owner || !repo || !loggedIn || !stars || starring) return;

    setStarring(true);
    setStarsError('');

    try {
      const next = stars.starred
        ? await unstarRepo(owner, repo)
        : await starRepo(owner, repo);
      setStars(next);
    } catch (e: any) {
      setStarsError(e?.message || 'failed to update star');
    } finally {
      setStarring(false);
    }
  };

  const onForkSubmit = async (e: Event) => {
    e.preventDefault();
    if (!owner || !repo || !loggedIn || forking) return;

    const requestedName = forkName.trim();

    setForking(true);
    setForkError('');
    setForkNotice('Creating fork...');

    try {
      const created = await forkRepo(owner, repo, requestedName || undefined);
      const targetOwner = (typeof created.owner_name === 'string' && created.owner_name.trim())
        ? created.owner_name
        : owner;
      const targetRepo = (typeof created.name === 'string' && created.name.trim())
        ? created.name
        : (requestedName || repo);

      setForks((current) => {
        const exists = current.some((item) => item.owner_name === targetOwner && item.name === targetRepo);
        if (exists) return current;
        return [{ ...created, owner_name: targetOwner, name: targetRepo }, ...current];
      });

      setForkPanelOpen(false);
      setForkNotice(`Fork created: ${targetOwner}/${targetRepo}. Redirecting...`);

      window.setTimeout(() => {
        window.location.assign(`/${targetOwner}/${targetRepo}`);
      }, 700);
    } catch (err: any) {
      setForkNotice('');
      setForkError(err?.message || 'failed to fork repository');
    } finally {
      setForking(false);
    }
  };

  return (
    <div>
      <div style={{ marginBottom: '20px' }}>
        {forkNotice && (
          <div
            style={{
              color: forking ? '#58a6ff' : '#3fb950',
              marginBottom: '12px',
              padding: '10px 12px',
              background: forking ? '#111b2e' : '#132a1d',
              border: `1px solid ${forking ? '#1f6feb' : '#3fb950'}`,
              borderRadius: '6px',
              fontSize: '13px',
            }}
          >
            {forkNotice}
          </div>
        )}

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', flexWrap: 'wrap' }}>
          <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '4px' }}>
            <a href={`/${owner}`} style={{ color: '#58a6ff' }}>{owner}</a>
            <span style={{ color: '#8b949e' }}> / </span>
            <a href={`/${owner}/${repo}`} style={{ color: '#58a6ff', fontWeight: 'bold' }}>{repo}</a>
            {repoInfo.is_private && (
              <span
                style={{
                  color: '#8b949e',
                  fontSize: '12px',
                  fontWeight: 'normal',
                  marginLeft: '8px',
                  border: '1px solid #30363d',
                  padding: '2px 8px',
                  borderRadius: '12px',
                }}
              >
                Private
              </span>
            )}
          </h1>

          <div style={{ display: 'flex', gap: '8px' }}>
            <button
              onClick={onToggleStar}
              disabled={!loggedIn || !stars || starring || starsLoading}
              style={{
                border: '1px solid #30363d',
                background: stars?.starred ? '#1f6feb' : '#161b22',
                color: stars?.starred ? '#fff' : '#c9d1d9',
                padding: '6px 12px',
                borderRadius: '6px',
                cursor: !loggedIn || starring || starsLoading ? 'not-allowed' : 'pointer',
                fontSize: '13px',
              }}
              title={loggedIn ? '' : 'Sign in to star this repository'}
            >
              {starsLoading ? 'Loading stars...' : starLabel}
            </button>

            <button
              onClick={openForkPanel}
              disabled={!loggedIn || forking}
              style={{
                border: '1px solid #30363d',
                background: forkPanelOpen ? '#1f6feb' : '#161b22',
                color: '#c9d1d9',
                padding: '6px 12px',
                borderRadius: '6px',
                cursor: !loggedIn || forking ? 'not-allowed' : 'pointer',
                fontSize: '13px',
              }}
              title={loggedIn ? '' : 'Sign in to fork this repository'}
            >
              {forking ? 'Forking...' : `Fork${forkCountLabel}`}
            </button>
          </div>
        </div>

        {repoInfo.description && <p style={{ color: '#8b949e', fontSize: '14px' }}>{repoInfo.description}</p>}

        {parentRepo && (
          <p style={{ color: '#8b949e', fontSize: '13px', marginTop: '8px', marginBottom: '0' }}>
            forked from{' '}
            <a href={`/${parentRepo.owner}/${parentRepo.name}`} style={{ color: '#58a6ff' }}>
              {parentRepo.owner}/{parentRepo.name}
            </a>
            {showUpstream && upstream && (
              <>
                <span style={{ color: '#8b949e' }}> · upstream </span>
                <a href={`/${upstream.owner}/${upstream.name}`} style={{ color: '#58a6ff' }}>
                  {upstream.owner}/{upstream.name}
                </a>
              </>
            )}
            {upstreamLoading && (
              <span style={{ color: '#8b949e' }}> · resolving upstream...</span>
            )}
          </p>
        )}

        {starsError && (
          <div
            style={{
              marginTop: '10px',
              color: '#f85149',
              fontSize: '13px',
              padding: '8px 10px',
              background: '#1c1214',
              border: '1px solid #f85149',
              borderRadius: '6px',
            }}
          >
            {starsError}
          </div>
        )}

        {forkPanelOpen && (
          <form
            onSubmit={onForkSubmit}
            style={{
              marginTop: '12px',
              border: '1px solid #30363d',
              borderRadius: '6px',
              padding: '12px',
              background: '#161b22',
              display: 'grid',
              gap: '10px',
              maxWidth: '520px',
            }}
          >
            <div style={{ color: '#f0f6fc', fontSize: '14px', fontWeight: 'bold' }}>
              Create fork
            </div>

            <label style={{ color: '#8b949e', fontSize: '13px' }}>
              Fork name
              <input
                value={forkName}
                onInput={(event: any) => setForkName(event.target.value)}
                placeholder={repo || ''}
                style={{
                  marginTop: '6px',
                  width: '100%',
                  background: '#0d1117',
                  border: '1px solid #30363d',
                  borderRadius: '6px',
                  padding: '9px 10px',
                  color: '#c9d1d9',
                  fontSize: '13px',
                  boxSizing: 'border-box',
                }}
              />
            </label>

            <div style={{ color: '#8b949e', fontSize: '12px' }}>
              Leave blank to use the default fork name. Existing names are auto-suffixed.
            </div>

            {forkError && (
              <div
                style={{
                  color: '#f85149',
                  fontSize: '13px',
                  padding: '8px 10px',
                  background: '#1c1214',
                  border: '1px solid #f85149',
                  borderRadius: '6px',
                }}
              >
                {forkError}
              </div>
            )}

            <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
              <button
                type="submit"
                disabled={forking}
                style={{
                  border: 'none',
                  background: '#238636',
                  color: '#fff',
                  padding: '8px 14px',
                  borderRadius: '6px',
                  cursor: forking ? 'not-allowed' : 'pointer',
                  fontSize: '13px',
                  fontWeight: 'bold',
                  opacity: forking ? 0.7 : 1,
                }}
              >
                {forking ? 'Creating fork...' : 'Create fork'}
              </button>

              <button
                type="button"
                onClick={() => {
                  if (forking) return;
                  setForkPanelOpen(false);
                  setForkError('');
                }}
                style={{
                  border: '1px solid #30363d',
                  background: '#0d1117',
                  color: '#c9d1d9',
                  padding: '8px 14px',
                  borderRadius: '6px',
                  cursor: forking ? 'not-allowed' : 'pointer',
                  fontSize: '13px',
                }}
              >
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>

      <div style={{ display: 'flex', gap: '12px', marginBottom: '20px', flexWrap: 'wrap' }}>
        <a href={`/${owner}/${repo}/tree/${defaultRef}`} style={tabStyle}>Code</a>
        <a href={`/${owner}/${repo}/commits/${defaultRef}`} style={tabStyle}>Commits</a>
        <a href={`/${owner}/${repo}/pulls`} style={tabStyle}>Pull requests</a>
        <a href={`/${owner}/${repo}/issues`} style={tabStyle}>Issues</a>
        <a href={`/${owner}/${repo}/symbols/${defaultRef}`} style={tabStyle}>Symbols</a>
        <a href={`/${owner}/${repo}/entity-history/${defaultRef}`} style={tabStyle}>Entity History</a>
        {loggedIn && <a href={`/${owner}/${repo}/settings`} style={tabStyle}>Settings</a>}
      </div>

      {treeLoading && (
        <div style={{ color: '#8b949e', padding: '20px' }}>
          Loading repository tree...
        </div>
      )}

      {!treeLoading && treeError && (
        <div
          style={{
            border: '1px solid #f85149',
            borderRadius: '6px',
            padding: '14px',
            background: '#1c1214',
            color: '#f85149',
            marginBottom: '20px',
          }}
        >
          <div style={{ marginBottom: '10px', fontSize: '13px' }}>{treeError}</div>
          <button
            onClick={() => loadTree(defaultRef)}
            style={{
              border: '1px solid #30363d',
              background: '#0d1117',
              color: '#c9d1d9',
              padding: '6px 10px',
              borderRadius: '6px',
              cursor: 'pointer',
              fontSize: '12px',
            }}
          >
            Retry tree load
          </button>
        </div>
      )}

      {!treeLoading && !treeError && entries.length > 0 && owner && repo && (
        <FileTree entries={entries} owner={owner} repo={repo} ref={defaultRef} currentPath="" />
      )}

      {!treeLoading && !treeError && entries.length === 0 && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '40px', textAlign: 'center' }}>
          <p style={{ color: '#8b949e', marginBottom: '16px' }}>This repository is empty</p>
          <div
            style={{
              background: '#161b22',
              padding: '16px',
              borderRadius: '6px',
              fontFamily: 'monospace',
              fontSize: '13px',
              color: '#c9d1d9',
              textAlign: 'left',
              display: 'inline-block',
            }}
          >
            <div>got init</div>
            <div>got add .</div>
            <div>got commit -m "initial commit"</div>
            <div>got remote add origin {location.origin}/got/{owner}/{repo}</div>
            <div>got push origin main</div>
          </div>
        </div>
      )}

      {(forksLoading || forksError || forks.length > 0) && (
        <div style={{ marginTop: '20px', border: '1px solid #30363d', borderRadius: '6px', padding: '12px 16px' }}>
          <div style={{ color: '#f0f6fc', fontSize: '14px', marginBottom: '8px' }}>Forks</div>

          {forksLoading && <div style={{ color: '#8b949e', fontSize: '13px' }}>Loading forks...</div>}

          {!forksLoading && forksError && (
            <div style={{ color: '#f85149', fontSize: '13px' }}>
              <span style={{ marginRight: '8px' }}>{forksError}</span>
              <button
                onClick={loadForks}
                style={{
                  border: '1px solid #30363d',
                  background: '#0d1117',
                  color: '#c9d1d9',
                  padding: '5px 8px',
                  borderRadius: '6px',
                  cursor: 'pointer',
                  fontSize: '12px',
                }}
              >
                Retry
              </button>
            </div>
          )}

          {!forksLoading && !forksError && forks.length === 0 && (
            <div style={{ color: '#8b949e', fontSize: '13px' }}>No forks yet.</div>
          )}

          {!forksLoading && !forksError && forks.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px' }}>
              {forks.slice(0, 12).map((item) => (
                <a key={item.id} href={`/${item.owner_name}/${item.name}`} style={{ color: '#58a6ff', fontSize: '13px' }}>
                  {item.owner_name}/{item.name}
                </a>
              ))}
              {forks.length > 12 && (
                <span style={{ color: '#8b949e', fontSize: '13px' }}>+{forks.length - 12} more</span>
              )}
            </div>
          )}
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
