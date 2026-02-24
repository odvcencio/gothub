import { useEffect, useState } from 'preact/hooks';
import { createIssueComment, getIssue, getToken, listIssueComments, updateIssue } from '../api/client';

interface Props {
  owner?: string;
  repo?: string;
  number?: string;
}

export function IssueDetailView({ owner, repo, number }: Props) {
  const [issue, setIssue] = useState<any>(null);
  const [comments, setComments] = useState<any[]>([]);
  const [body, setBody] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const loggedIn = !!getToken();
  const issueNumber = Number(number || 0);

  const loadIssue = () => {
    if (!owner || !repo || !issueNumber) return;
    getIssue(owner, repo, issueNumber).then(setIssue).catch((e) => setError(e.message));
    listIssueComments(owner, repo, issueNumber).then(setComments).catch(() => {});
  };

  useEffect(() => {
    loadIssue();
  }, [owner, repo, issueNumber]);

  const onComment = async (e: Event) => {
    e.preventDefault();
    if (!owner || !repo || !issueNumber || !body.trim()) return;
    setSaving(true);
    try {
      const c = await createIssueComment(owner, repo, issueNumber, { body });
      setComments([...comments, c]);
      setBody('');
    } catch (err: any) {
      setError(err.message || 'failed to comment');
    } finally {
      setSaving(false);
    }
  };

  const onSetState = async (nextState: 'open' | 'closed') => {
    if (!owner || !repo || !issueNumber || !issue) return;
    setSaving(true);
    try {
      const updated = await updateIssue(owner, repo, issueNumber, { state: nextState });
      setIssue(updated);
    } catch (err: any) {
      setError(err.message || 'failed to update issue');
    } finally {
      setSaving(false);
    }
  };

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  if (!issue) return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;

  return (
    <div>
      <a href={`/${owner}/${repo}/issues`} style={{ color: '#58a6ff', fontSize: '13px', textDecoration: 'none' }}>
        Back to issues
      </a>

      <div style={{ marginTop: '10px', marginBottom: '16px' }}>
        <h1 style={{ fontSize: '20px', color: '#f0f6fc', marginBottom: '6px' }}>
          {issue.title} <span style={{ color: '#8b949e', fontWeight: 'normal' }}>#{issue.number}</span>
        </h1>
        <div style={{ color: '#8b949e', fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
          <span
            style={{
              color: '#fff',
              background: issue.state === 'open' ? '#238636' : '#da3633',
              borderRadius: '12px',
              padding: '2px 8px',
              fontWeight: 'bold',
              fontSize: '12px',
            }}
          >
            {issue.state}
          </span>
          <span>opened by {issue.author_name || 'user'}</span>
          {loggedIn && (
            <button
              onClick={() => onSetState(issue.state === 'open' ? 'closed' : 'open')}
              disabled={saving}
              style={{ border: '1px solid #30363d', background: '#161b22', color: '#c9d1d9', borderRadius: '6px', padding: '5px 9px', cursor: saving ? 'not-allowed' : 'pointer', fontSize: '12px' }}
            >
              {issue.state === 'open' ? 'Close issue' : 'Reopen issue'}
            </button>
          )}
        </div>
      </div>

      {issue.body && (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px', marginBottom: '12px', whiteSpace: 'pre-wrap', color: '#c9d1d9', fontSize: '13px' }}>
          {issue.body}
        </div>
      )}

      <div style={{ border: '1px solid #30363d', borderRadius: '6px', marginBottom: '12px' }}>
        {comments.length === 0 ? (
          <div style={{ color: '#8b949e', padding: '12px', fontSize: '13px' }}>No comments yet</div>
        ) : (
          comments.map((comment, idx) => (
            <div key={comment.id || idx} style={{ borderTop: idx === 0 ? 'none' : '1px solid #30363d', padding: '10px 12px' }}>
              <div style={{ color: '#8b949e', fontSize: '12px', marginBottom: '6px' }}>
                {comment.author_name || 'user'}
              </div>
              <div style={{ color: '#c9d1d9', fontSize: '13px', whiteSpace: 'pre-wrap' }}>{comment.body}</div>
            </div>
          ))
        )}
      </div>

      {loggedIn && (
        <form onSubmit={onComment} style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '12px' }}>
          <textarea
            value={body}
            onInput={(e: any) => setBody(e.target.value)}
            placeholder="Leave a comment"
            style={{ width: '100%', minHeight: '90px', boxSizing: 'border-box', background: '#0d1117', color: '#c9d1d9', border: '1px solid #30363d', borderRadius: '6px', padding: '8px 10px', fontSize: '13px', resize: 'vertical' }}
          />
          <div style={{ marginTop: '8px', display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="submit"
              disabled={saving || !body.trim()}
              style={{ background: '#238636', color: '#fff', border: 'none', borderRadius: '6px', padding: '6px 12px', cursor: saving ? 'not-allowed' : 'pointer' }}
            >
              Comment
            </button>
          </div>
        </form>
      )}
    </div>
  );
}
