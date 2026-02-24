import { useState, useEffect } from 'preact/hooks';
import { getPR, getPRDiff, getMergePreview, getMergeGate, mergePR, listPRComments, createPRComment, listPRReviews, createPRReview, listPRChecks, getToken } from '../api/client';
import { EntityDiff } from '../components/EntityDiff';
import { MergePreview } from '../components/MergePreview';
import { ConflictViewer } from '../components/ConflictViewer';

interface Props {
  owner?: string;
  repo?: string;
  number?: string;
}

export function PRDetailView({ owner, repo, number }: Props) {
  const [pr, setPr] = useState<any>(null);
  const [tab, setTab] = useState<'conversation' | 'files' | 'merge'>('conversation');
  const [diff, setDiff] = useState<any>(null);
  const [mergePreview, setMergePreview] = useState<any>(null);
  const [mergeGate, setMergeGate] = useState<{ allowed: boolean; reasons?: string[] } | null>(null);
  const [checks, setChecks] = useState<any[]>([]);
  const [comments, setComments] = useState<any[]>([]);
  const [reviews, setReviews] = useState<any[]>([]);
  const [merging, setMerging] = useState(false);
  const [error, setError] = useState('');

  const prNum = Number(number);

  useEffect(() => {
    if (!owner || !repo || !prNum) return;
    getPR(owner, repo, prNum).then(setPr).catch(e => setError(e.message));
    listPRComments(owner, repo, prNum).then(setComments).catch(() => {});
    listPRReviews(owner, repo, prNum).then(setReviews).catch(() => {});
  }, [owner, repo, prNum]);

  useEffect(() => {
    if (!owner || !repo || !prNum) return;
    if (tab === 'files' && !diff) {
      getPRDiff(owner, repo, prNum).then(setDiff).catch(() => {});
    }
    if (tab === 'merge' && !mergePreview) {
      getMergePreview(owner, repo, prNum).then(setMergePreview).catch(() => {});
    }
    if (tab === 'merge') {
      getMergeGate(owner, repo, prNum).then(setMergeGate).catch(() => {});
      listPRChecks(owner, repo, prNum).then(setChecks).catch(() => {});
    }
  }, [tab, owner, repo, prNum, reviews.length]);

  const handleMerge = async () => {
    setMerging(true);
    try {
      await mergePR(owner!, repo!, prNum);
      getPR(owner!, repo!, prNum).then(setPr);
      getMergeGate(owner!, repo!, prNum).then(setMergeGate).catch(() => {});
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

      {tab === 'conversation' && (
        <ConversationTab pr={pr} comments={comments} reviews={reviews}
          owner={owner!} repo={repo!} prNum={prNum}
          onCommentAdded={(c: any) => setComments([...comments, c])}
          onReviewAdded={(r: any) => setReviews([...reviews, r])} />
      )}

      {tab === 'files' && (
        diff ? <EntityDiff files={diff.files || []} /> : <div style={{ color: '#8b949e' }}>Loading diff...</div>
      )}

      {tab === 'merge' && (
        <div style={{ display: 'grid', gap: '16px' }}>
          <MergeGatePanel gate={mergeGate} checks={checks} />
          {mergePreview
            ? <>
                <MergePreview preview={mergePreview} onMerge={pr.state === 'open' ? handleMerge : undefined} merging={merging} />
                <ConflictViewer files={mergePreview.files || []} />
              </>
            : <div style={{ color: '#8b949e' }}>Loading merge preview...</div>}
        </div>
      )}
    </div>
  );
}

function MergeGatePanel({ gate, checks }: { gate: { allowed: boolean; reasons?: string[] } | null; checks: any[] }) {
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

function ConversationTab({ pr, comments, reviews, owner, repo, prNum, onCommentAdded, onReviewAdded }: {
  pr: any; comments: any[]; reviews: any[]; owner: string; repo: string; prNum: number;
  onCommentAdded: (c: any) => void; onReviewAdded: (r: any) => void;
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
    } catch {}
    setSubmitting(false);
  };

  const handleReview = async (state: string) => {
    setSubmitting(true);
    try {
      const r = await createPRReview(owner, repo, prNum, { state, body: body || undefined });
      onReviewAdded(r);
      setBody('');
    } catch {}
    setSubmitting(false);
  };

  // Merge comments and reviews into a timeline
  const timeline = [
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
