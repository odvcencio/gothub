import { useState, useEffect, useMemo } from 'preact/hooks';
import {
  createPRComment,
  createPRReview,
  getMergeGate,
  getMergePreview,
  getPR,
  getPRDiff,
  getRepoIndexStatus,
  getSemver,
  getToken,
  listPRChecks,
  listPRComments,
  listPRReviews,
  mergePR,
  type CheckRun,
  type DiffFile,
  type MergeGate,
  type MergePreviewResponse,
  type PRComment,
  type PRReview,
  type PullRequest,
  type RepoIndexStatus,
  type SemverRecommendation,
} from '../api/client';
import { EntityDiff } from '../components/EntityDiff';
import { MergePreview } from '../components/MergePreview';
import { ConflictViewer } from '../components/ConflictViewer';
import { IndexStatusCard } from '../components/IndexStatusCard';

interface Props {
  owner?: string;
  repo?: string;
  number?: string;
}

export function PRDetailView({ owner, repo, number }: Props) {
  const [pr, setPr] = useState<PullRequest | null>(null);
  const [tab, setTab] = useState<'conversation' | 'files' | 'merge'>('conversation');
  const [diff, setDiff] = useState<{ files: DiffFile[] } | null>(null);
  const [diffError, setDiffError] = useState('');
  const [mergePreview, setMergePreview] = useState<MergePreviewResponse | null>(null);
  const [mergePreviewError, setMergePreviewError] = useState('');
  const [mergeGate, setMergeGate] = useState<MergeGate | null>(null);
  const [semver, setSemver] = useState<SemverRecommendation | null>(null);
  const [semverError, setSemverError] = useState('');
  const [checks, setChecks] = useState<CheckRun[]>([]);
  const [indexStatus, setIndexStatus] = useState<RepoIndexStatus | null>(null);
  const [indexStatusLoading, setIndexStatusLoading] = useState(false);
  const [indexStatusError, setIndexStatusError] = useState('');
  const [comments, setComments] = useState<PRComment[]>([]);
  const [reviews, setReviews] = useState<PRReview[]>([]);
  const [merging, setMerging] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const appendNotice = (message: string) => {
    const next = message.trim();
    if (!next) return;
    setNotice((current) => {
      if (!current) return next;
      if (current.includes(next)) return current;
      return `${current} • ${next}`;
    });
  };

  const prNum = Number(number);

  useEffect(() => {
    setPr(null);
    setDiff(null);
    setDiffError('');
    setMergePreview(null);
    setMergePreviewError('');
    setMergeGate(null);
    setSemver(null);
    setSemverError('');
    setChecks([]);
    setIndexStatus(null);
    setIndexStatusLoading(false);
    setIndexStatusError('');
    setComments([]);
    setReviews([]);
    setMerging(false);
    setError('');
    setNotice('');
  }, [owner, repo, prNum]);

  useEffect(() => {
    if (!owner || !repo || !prNum) return;
    getPR(owner, repo, prNum).then(setPr).catch(e => setError(e.message));
    listPRComments(owner, repo, prNum).then(setComments).catch(e => appendNotice(e.message || 'failed to load comments'));
    listPRReviews(owner, repo, prNum).then(setReviews).catch(e => appendNotice(e.message || 'failed to load reviews'));
  }, [owner, repo, prNum]);

  useEffect(() => {
    if (!owner || !repo || !prNum) return;
    if ((tab === 'files' || tab === 'merge') && !diff && !diffError) {
      getPRDiff(owner, repo, prNum)
        .then((data) => {
          setDiff(data);
          setDiffError('');
        })
        .catch((e) => {
          const message = e?.message || 'failed to load diff';
          setDiffError(message);
          appendNotice(message);
        });
    }
    if (tab === 'merge' && !mergePreview && !mergePreviewError) {
      getMergePreview(owner, repo, prNum)
        .then((preview) => {
          setMergePreview(preview);
          setMergePreviewError('');
        })
        .catch((e) => {
          const message = e?.message || 'failed to load merge preview';
          setMergePreviewError(message);
          appendNotice(message);
        });
    }
    if (tab === 'merge') {
      getMergeGate(owner, repo, prNum).then(setMergeGate).catch(e => appendNotice(e.message || 'failed to load merge gate'));
      listPRChecks(owner, repo, prNum).then(setChecks).catch(e => appendNotice(e.message || 'failed to load checks'));
      if (pr && !semver && !semverError) {
        const spec = `${pr.target_branch}...${pr.source_branch}`;
        getSemver(owner, repo, spec)
          .then((recommendation) => {
            setSemver(recommendation);
            setSemverError('');
          })
          .catch((e) => {
            const message = e?.message || 'failed to load semver recommendation';
            setSemverError(message);
            appendNotice(message);
          });
      }
    }
  }, [tab, owner, repo, prNum, pr, reviews.length, semver]);

  useEffect(() => {
    if (!owner || !repo || !pr?.source_branch) return;

    let cancelled = false;
    setIndexStatus(null);
    setIndexStatusError('');
    setIndexStatusLoading(true);

    getRepoIndexStatus(owner, repo, pr.source_branch)
      .then((status) => {
        if (!cancelled) setIndexStatus(status);
      })
      .catch((e: any) => {
        if (!cancelled) setIndexStatusError(e?.message || 'failed to load indexing status');
      })
      .finally(() => {
        if (!cancelled) setIndexStatusLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [owner, repo, pr?.source_branch]);

  const impactSummary = useMemo(() => summarizeDiff(diff?.files || []), [diff]);
  const showIndexStatus = indexStatusLoading || !!indexStatusError || !!indexStatus;

  const handleMerge = async () => {
    if (!owner || !repo || !prNum) {
      setError('missing repository context');
      return;
    }
    setMerging(true);
    try {
      await mergePR(owner, repo, prNum);
      getPR(owner, repo, prNum).then(setPr).catch(e => appendNotice(e.message || 'failed to refresh pull request'));
      getMergeGate(owner, repo, prNum).then(setMergeGate).catch(e => appendNotice(e.message || 'failed to refresh merge gate'));
    } catch (e: any) {
      setError(e.message);
    }
    setMerging(false);
  };

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  if (!pr) return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;

  return (
    <div>
      <div style={{ marginBottom: '20px' }}>
        <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '4px' }}>
          {pr.title} <span style={{ color: '#8b949e', fontWeight: 'normal' }}>#{pr.number}</span>
        </h1>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px' }}>
          <span style={{
            background: pr.state === 'open' ? '#238636' : pr.state === 'merged' ? '#8957e5' : '#da3633',
            color: '#fff', padding: '4px 10px', borderRadius: '12px', fontWeight: 'bold'
          }}>
            {pr.state}
          </span>
          <span style={{ color: '#8b949e' }}>
            wants to merge <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px' }}>{pr.source_branch}</code> into <code style={{ background: '#161b22', padding: '2px 6px', borderRadius: '4px' }}>{pr.target_branch}</code>
          </span>
        </div>
        {showIndexStatus && (
          <div style={{ marginTop: '12px', maxWidth: '420px' }}>
            <IndexStatusCard
              status={indexStatus}
              loading={indexStatusLoading}
              error={indexStatusError}
              refName={pr.source_branch}
            />
          </div>
        )}
      </div>

      <div style={{ display: 'flex', gap: '4px', marginBottom: '20px', borderBottom: '1px solid #30363d', paddingBottom: '0' }}>
        {(['conversation', 'files', 'merge'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)}
            style={{
              background: 'transparent',
              color: tab === t ? '#f0f6fc' : '#8b949e',
              border: 'none',
              borderBottom: tab === t ? '2px solid #f78166' : '2px solid transparent',
              padding: '8px 16px',
              cursor: 'pointer',
              fontSize: '14px',
            }}>
            {t === 'conversation' ? 'Conversation' : t === 'files' ? 'Files changed' : 'Merge preview'}
          </button>
        ))}
      </div>

      {notice && (
        <div style={{ color: '#d29922', marginBottom: '16px', padding: '10px 12px', background: '#2b230f', border: '1px solid #d29922', borderRadius: '6px', fontSize: '13px' }}>
          {notice}
        </div>
      )}

      {tab === 'conversation' && (
        owner && repo ? (
          <ConversationTab
            pr={pr}
            comments={comments}
            reviews={reviews}
            owner={owner}
            repo={repo}
            prNum={prNum}
            onCommentAdded={(c: any) => setComments([...comments, c])}
            onReviewAdded={(r: any) => setReviews([...reviews, r])}
            onError={(message: string) => appendNotice(message)}
          />
        ) : (
          <div style={{ color: '#f85149' }}>Missing repository context</div>
        )
      )}

      {tab === 'files' && (
        diff ? (
          <div style={{ display: 'grid', gap: '16px' }}>
            <PRImpactSummary summary={impactSummary} />
            <EntityDiff files={diff.files || []} />
          </div>
        ) : diffError ? (
          <div style={{ color: '#f85149', border: '1px solid #f85149', borderRadius: '6px', padding: '12px 14px', background: '#1c1214' }}>
            {diffError}
          </div>
        ) : <div style={{ color: '#8b949e' }}>Loading diff...</div>
      )}

      {tab === 'merge' && (
        <div style={{ display: 'grid', gap: '16px' }}>
          <PRImpactSummary summary={impactSummary} />
          {semverError ? (
            <div style={{ border: '1px solid #f85149', borderRadius: '6px', padding: '12px 16px', color: '#f85149', background: '#1c1214', fontSize: '13px' }}>
              {semverError}
            </div>
          ) : (
            <SemverPanel semver={semver} />
          )}
          <MergeGatePanel gate={mergeGate} checks={checks} />
          {mergePreview
            ? <>
                <MergePreview preview={mergePreview} onMerge={pr.state === 'open' ? handleMerge : undefined} merging={merging} />
                <ConflictViewer files={mergePreview.files || []} />
              </>
            : mergePreviewError ? (
              <div style={{ color: '#f85149', border: '1px solid #f85149', borderRadius: '6px', padding: '12px 14px', background: '#1c1214' }}>
                {mergePreviewError}
              </div>
            ) : <div style={{ color: '#8b949e' }}>Loading merge preview...</div>}
        </div>
      )}
    </div>
  );
}

function MergeGatePanel({ gate, checks }: { gate: MergeGate | null; checks: CheckRun[] }) {
  const ownerApprovals = gate?.entity_owner_approvals || [];

  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
      <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <strong style={{ color: '#f0f6fc', fontSize: '14px' }}>Merge Gate</strong>
        <span style={{ color: gate?.allowed ? '#3fb950' : '#f85149', fontSize: '13px', fontWeight: 'bold' }}>
          {gate == null ? 'checking...' : gate.allowed ? 'passing' : 'blocked'}
        </span>
      </div>

      {gate && !gate.allowed && gate.reasons && gate.reasons.length > 0 && (
        <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d' }}>
          <div style={{ color: '#f85149', fontSize: '13px', marginBottom: '6px' }}>Blocking reasons</div>
          {gate.reasons.map((reason, idx) => (
            <div key={idx} style={{ color: '#c9d1d9', fontSize: '13px' }}>- {reason}</div>
          ))}
        </div>
      )}

      {gate && ownerApprovals.length > 0 && (
        <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d' }}>
          <div style={{ color: '#8b949e', fontSize: '12px', marginBottom: '8px' }}>Entity owner approvals</div>
          {ownerApprovals.map(approval => (
            <div key={`${approval.path}:${approval.entity_key}`} style={{ padding: '8px 0', borderTop: '1px solid #21262d' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px' }}>
                <span style={{ fontFamily: 'monospace', fontSize: '13px', color: '#c9d1d9' }}>{approval.entity_key}</span>
                <span style={{ color: approval.satisfied ? '#3fb950' : '#f85149', fontSize: '12px', fontWeight: 'bold' }}>
                  {approval.satisfied ? 'approved' : 'awaiting'}
                </span>
              </div>
              <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '4px' }}>{approval.path}</div>
              {approval.required_owners && approval.required_owners.length > 0 && (
                <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '4px' }}>
                  required: {formatMentions(approval.required_owners)}
                </div>
              )}
              {approval.approved_by && approval.approved_by.length > 0 && (
                <div style={{ color: '#3fb950', fontSize: '12px', marginTop: '2px' }}>
                  approved by: {formatMentions(approval.approved_by)}
                </div>
              )}
              {approval.missing_owners && approval.missing_owners.length > 0 && (
                <div style={{ color: '#f85149', fontSize: '12px', marginTop: '2px' }}>
                  missing: {formatMentions(approval.missing_owners)}
                </div>
              )}
              {approval.unresolved_teams && approval.unresolved_teams.length > 0 && (
                <div style={{ color: '#d29922', fontSize: '12px', marginTop: '2px' }}>
                  unresolved teams: {approval.unresolved_teams.map(team => `@team/${team}`).join(', ')}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <div style={{ padding: '12px 16px' }}>
        <div style={{ color: '#8b949e', fontSize: '12px', marginBottom: '8px' }}>Check runs</div>
        {checks.length === 0 ? (
          <div style={{ color: '#8b949e', fontSize: '13px' }}>No checks reported</div>
        ) : (
          checks.map((check, idx) => (
            <div key={idx} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 0', borderTop: idx === 0 ? 'none' : '1px solid #21262d' }}>
              <span style={{ color: '#c9d1d9', fontSize: '13px', fontFamily: 'monospace' }}>{check.name}</span>
              <span style={{ color: check.status === 'completed' && check.conclusion === 'success' ? '#3fb950' : check.status === 'completed' ? '#f85149' : '#d29922', fontSize: '12px' }}>
                {check.status}{check.conclusion ? `/${check.conclusion}` : ''}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function PRImpactSummary({ summary }: { summary: ImpactSummary }) {
  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
      <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d' }}>
        <strong style={{ color: '#f0f6fc', fontSize: '14px' }}>PR Impact</strong>
      </div>
      <div style={{ padding: '12px 16px', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(110px, 1fr))', gap: '10px' }}>
        <ImpactStat label="Files" value={summary.files} color="#8b949e" />
        <ImpactStat label="Entities" value={summary.entities} color="#8b949e" />
        <ImpactStat label="Added" value={summary.added} color="#3fb950" />
        <ImpactStat label="Modified" value={summary.modified} color="#d29922" />
        <ImpactStat label="Removed" value={summary.removed} color="#f85149" />
      </div>
    </div>
  );
}

function ImpactStat({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div style={{ border: '1px solid #21262d', borderRadius: '6px', padding: '10px 12px', background: '#0d1117' }}>
      <div style={{ color: '#8b949e', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.04em', marginBottom: '4px' }}>
        {label}
      </div>
      <div style={{ color, fontSize: '20px', fontWeight: 'bold', lineHeight: 1 }}>{value}</div>
    </div>
  );
}

function SemverPanel({ semver }: { semver: SemverRecommendation | null }) {
  if (!semver) {
    return (
      <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px 16px', color: '#8b949e', fontSize: '13px' }}>
        Loading semver recommendation...
      </div>
    );
  }

  const bump = (semver.bump || 'none').toLowerCase();
  const bumpColor = bump === 'major' ? '#f85149' : bump === 'minor' ? '#d29922' : bump === 'patch' ? '#3fb950' : '#8b949e';

  return (
    <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
      <div style={{ padding: '12px 16px', borderBottom: '1px solid #30363d', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <strong style={{ color: '#f0f6fc', fontSize: '14px' }}>SemVer Recommendation</strong>
        <span style={{ color: bumpColor, fontWeight: 'bold', fontSize: '13px', textTransform: 'uppercase' }}>{bump}</span>
      </div>
      <div style={{ padding: '12px 16px', display: 'grid', gap: '8px' }}>
        {semver.breaking_changes && semver.breaking_changes.length > 0 && (
          <SemverDetail title="Breaking changes" color="#f85149" items={semver.breaking_changes} />
        )}
        {semver.features && semver.features.length > 0 && (
          <SemverDetail title="Features" color="#d29922" items={semver.features} />
        )}
        {semver.fixes && semver.fixes.length > 0 && (
          <SemverDetail title="Fixes" color="#3fb950" items={semver.fixes} />
        )}
        {(!semver.breaking_changes || semver.breaking_changes.length === 0) &&
          (!semver.features || semver.features.length === 0) &&
          (!semver.fixes || semver.fixes.length === 0) && (
            <div style={{ color: '#8b949e', fontSize: '13px' }}>
              No structural export surface changes detected.
            </div>
          )}
      </div>
    </div>
  );
}

function SemverDetail({ title, color, items }: { title: string; color: string; items: string[] }) {
  return (
    <div>
      <div style={{ color, fontSize: '12px', fontWeight: 'bold', marginBottom: '4px' }}>{title}</div>
      {items.slice(0, 4).map(item => (
        <div key={item} style={{ color: '#c9d1d9', fontSize: '13px' }}>
          - {item}
        </div>
      ))}
      {items.length > 4 && (
        <div style={{ color: '#8b949e', fontSize: '12px', marginTop: '2px' }}>
          +{items.length - 4} more
        </div>
      )}
    </div>
  );
}

type ImpactSummary = {
  files: number;
  entities: number;
  added: number;
  modified: number;
  removed: number;
};

function summarizeDiff(files: DiffFile[]): ImpactSummary {
  const summary: ImpactSummary = {
    files: files.length,
    entities: 0,
    added: 0,
    modified: 0,
    removed: 0,
  };
  for (const file of files) {
    for (const change of file.changes || []) {
      summary.entities++;
      if (change.type === 'added') summary.added++;
      else if (change.type === 'modified') summary.modified++;
      else if (change.type === 'removed') summary.removed++;
    }
  }
  return summary;
}

function formatMentions(users: string[] | undefined): string {
  if (!users || users.length === 0) return '';
  return users.map(user => `@${user}`).join(', ');
}

function ConversationTab({ pr, comments, reviews, owner, repo, prNum, onCommentAdded, onReviewAdded, onError }: {
  pr: PullRequest; comments: PRComment[]; reviews: PRReview[]; owner: string; repo: string; prNum: number;
  onCommentAdded: (c: PRComment) => void; onReviewAdded: (r: PRReview) => void;
  onError: (message: string) => void;
}) {
  const [body, setBody] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const loggedIn = !!getToken();

  const handleComment = async (e: Event) => {
    e.preventDefault();
    if (!body.trim()) return;
    setSubmitting(true);
    try {
      const c = await createPRComment(owner, repo, prNum, { body });
      onCommentAdded(c);
      setBody('');
    } catch (err: any) {
      onError(err.message || 'failed to submit comment');
    }
    setSubmitting(false);
  };

  const handleReview = async (state: string) => {
    setSubmitting(true);
    try {
      const r = await createPRReview(owner, repo, prNum, { state, body: body || undefined });
      onReviewAdded(r);
      setBody('');
    } catch (err: any) {
      onError(err.message || 'failed to submit review');
    }
    setSubmitting(false);
  };

  // Merge comments and reviews into a timeline
  const timeline: any[] = [
    ...comments.map(c => ({ ...c, kind: 'comment', time: c.created_at })),
    ...reviews.map(r => ({ ...r, kind: 'review', time: r.created_at })),
  ].sort((a, b) => (a.time || '').localeCompare(b.time || ''));

  return (
    <div>
      {pr.body && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginBottom: '16px' }}>
          <div style={{ color: '#c9d1d9', fontSize: '14px', whiteSpace: 'pre-wrap' }}>{pr.body}</div>
        </div>
      )}

      {timeline.map((item, i) => (
        <div key={i} style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px 16px', marginBottom: '8px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px', fontSize: '12px', color: '#8b949e' }}>
            <strong style={{ color: '#c9d1d9' }}>{item.author_name || 'user'}</strong>
            {item.kind === 'review' && (
              <span style={{
                color: item.state === 'approved' ? '#3fb950' : item.state === 'changes_requested' ? '#f85149' : '#d29922',
                fontWeight: 'bold',
              }}>
                {item.state === 'approved' ? 'approved' : item.state === 'changes_requested' ? 'requested changes' : 'commented'}
              </span>
            )}
            {item.file_path && <span style={{ fontFamily: 'monospace' }}>{item.file_path}</span>}
            {item.entity_stable_id && <span style={{ fontFamily: 'monospace', color: '#58a6ff' }}>stable:{item.entity_stable_id}</span>}
            {item.entity_key && <span style={{ fontFamily: 'monospace', color: '#d2a8ff' }}>{item.entity_key}</span>}
          </div>
          {item.body && <div style={{ color: '#c9d1d9', fontSize: '13px', whiteSpace: 'pre-wrap' }}>{item.body}</div>}
        </div>
      ))}

      {loggedIn && pr.state === 'open' && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', marginTop: '16px' }}>
          <textarea value={body} onInput={(e: any) => setBody(e.target.value)}
            placeholder="Leave a comment..."
            style={{ width: '100%', minHeight: '80px', background: '#0d1117', border: '1px solid #30363d', borderRadius: '6px', padding: '10px', color: '#c9d1d9', fontSize: '13px', resize: 'vertical', boxSizing: 'border-box' }} />
          <div style={{ display: 'flex', gap: '8px', marginTop: '8px', justifyContent: 'flex-end' }}>
            <button onClick={() => handleReview('approved')} disabled={submitting}
              style={{ background: '#238636', color: '#fff', border: 'none', padding: '6px 12px', borderRadius: '6px', cursor: 'pointer', fontSize: '13px' }}>
              Approve
            </button>
            <button onClick={() => handleReview('changes_requested')} disabled={submitting}
              style={{ background: '#da3633', color: '#fff', border: 'none', padding: '6px 12px', borderRadius: '6px', cursor: 'pointer', fontSize: '13px' }}>
              Request changes
            </button>
            <button onClick={handleComment} disabled={submitting || !body.trim()}
              style={{ background: '#30363d', color: '#c9d1d9', border: 'none', padding: '6px 12px', borderRadius: '6px', cursor: 'pointer', fontSize: '13px' }}>
              Comment
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
