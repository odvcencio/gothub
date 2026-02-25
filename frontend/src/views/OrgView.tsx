import { useState, useEffect } from 'preact/hooks';
import { getOrg, listOrgMembers, listOrgRepos, addOrgMember, removeOrgMember, deleteOrg, getToken } from '../api/client';

interface Props {
  org?: string;
  path?: string;
}

export function OrgDetailView({ org }: Props) {
  const [orgInfo, setOrgInfo] = useState<any>(null);
  const [repos, setRepos] = useState<any[]>([]);
  const [members, setMembers] = useState<any[]>([]);
  const [error, setError] = useState('');

  // Add member form state
  const [newUsername, setNewUsername] = useState('');
  const [newRole, setNewRole] = useState('member');
  const [addingMember, setAddingMember] = useState(false);
  const [memberError, setMemberError] = useState('');

  // Delete org state
  const [confirmName, setConfirmName] = useState('');
  const [deleting, setDeleting] = useState(false);

  const loggedIn = !!getToken();

  useEffect(() => {
    if (!org) return;
    getOrg(org).then(setOrgInfo).catch(e => setError(e.message));
    listOrgRepos(org).then(setRepos).catch(e => setError(e.message || 'failed to load repositories'));
    listOrgMembers(org).then(setMembers).catch(e => setError(e.message || 'failed to load members'));
  }, [org]);

  const handleAddMember = async (e: Event) => {
    e.preventDefault();
    if (!org || !newUsername.trim()) return;
    setAddingMember(true);
    setMemberError('');
    try {
      await addOrgMember(org, newUsername.trim(), newRole);
      const updated = await listOrgMembers(org);
      setMembers(updated);
      setNewUsername('');
      setNewRole('member');
    } catch (err: any) {
      setMemberError(err.message || 'Failed to add member');
    } finally {
      setAddingMember(false);
    }
  };

  const handleRemoveMember = async (username: string) => {
    if (!org) return;
    if (!confirm(`Remove member "${username}" from ${org}?`)) return;
    try {
      await removeOrgMember(org, username);
      setMembers(members.filter(m => m.username !== username));
    } catch (err: any) {
      setMemberError(err.message || 'Failed to remove member');
    }
  };

  const handleDeleteOrg = async () => {
    if (!org || confirmName !== org) return;
    if (!confirm(`Are you sure you want to delete "${org}"? This action cannot be undone.`)) return;
    setDeleting(true);
    try {
      await deleteOrg(org);
      location.href = '/';
    } catch (err: any) {
      setError(err.message || 'Failed to delete organization');
      setDeleting(false);
    }
  };

  if (error) return <div style={{ color: '#f85149', padding: '20px' }}>{error}</div>;
  if (!orgInfo) return <div style={{ color: '#8b949e', padding: '20px' }}>Loading...</div>;

  return (
    <div>
      {/* Org header */}
      <div style={{ marginBottom: '24px' }}>
        <h1 style={{ fontSize: '24px', color: '#f0f6fc', marginBottom: '4px' }}>
          {orgInfo.display_name || orgInfo.name}
        </h1>
        {orgInfo.display_name && orgInfo.display_name !== orgInfo.name && (
          <div style={{ color: '#8b949e', fontSize: '14px' }}>{orgInfo.name}</div>
        )}
      </div>

      {/* Repositories section */}
      <div style={{ marginBottom: '32px' }}>
        <h2 style={{ fontSize: '18px', color: '#f0f6fc', marginBottom: '12px' }}>Repositories</h2>
        {repos.length === 0 ? (
          <div style={{ color: '#8b949e', padding: '20px 0', fontSize: '14px' }}>No repositories yet</div>
        ) : (
          <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
            {repos.map((repo, idx) => {
              const repoOwner = repo.owner_name || org || '';
              return (
                <div
                  key={repo.id}
                  style={{
                    display: 'block',
                    padding: '12px 16px',
                    borderTop: idx === 0 ? 'none' : '1px solid #21262d',
                  }}
                >
                  <div style={{ marginBottom: '6px' }}>
                    <a
                      href={`/${repoOwner}/${repo.name}`}
                      style={{
                        color: '#58a6ff',
                        textDecoration: 'none',
                        fontWeight: 'bold',
                        fontSize: '14px',
                      }}
                    >
                      {repo.name}
                    </a>
                    {repo.is_private && (
                      <span style={{
                        color: '#8b949e',
                        fontSize: '11px',
                        fontWeight: 'normal',
                        marginLeft: '8px',
                        border: '1px solid #30363d',
                        padding: '1px 6px',
                        borderRadius: '12px',
                      }}>Private</span>
                    )}
                  </div>
                  {repo.description && (
                    <div style={{ color: '#8b949e', fontWeight: 'normal', fontSize: '13px' }}>
                      {repo.description}
                    </div>
                  )}
                  {repo.parent_owner && repo.parent_name && (
                    <div style={{ color: '#8b949e', fontWeight: 'normal', marginTop: '4px', fontSize: '13px' }}>
                      forked from{' '}
                      <a href={`/${repo.parent_owner}/${repo.parent_name}`} style={{ color: '#58a6ff', textDecoration: 'none' }}>
                        {repo.parent_owner}/{repo.parent_name}
                      </a>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Members section */}
      <div style={{ marginBottom: '32px' }}>
        <h2 style={{ fontSize: '18px', color: '#f0f6fc', marginBottom: '12px' }}>Members</h2>
        {memberError && (
          <div style={{ color: '#f85149', marginBottom: '12px', padding: '10px 12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px', fontSize: '13px' }}>
            {memberError}
          </div>
        )}
        {members.length === 0 ? (
          <div style={{ color: '#8b949e', padding: '20px 0', fontSize: '14px' }}>No members</div>
        ) : (
          <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
            {members.map((member, idx) => (
              <div
                key={member.user_id || idx}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '10px 16px',
                  borderTop: idx === 0 ? 'none' : '1px solid #21262d',
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                  <span style={{ color: '#c9d1d9', fontSize: '14px', fontWeight: 'bold' }}>{member.username}</span>
                  <span style={{
                    fontSize: '11px',
                    fontWeight: 'bold',
                    padding: '2px 8px',
                    borderRadius: '12px',
                    color: '#fff',
                    background: member.role === 'owner' ? '#238636' : '#30363d',
                  }}>
                    {member.role}
                  </span>
                </div>
                {loggedIn && (
                  <button
                    onClick={() => handleRemoveMember(member.username)}
                    style={{
                      background: 'transparent',
                      color: '#f85149',
                      border: '1px solid #f85149',
                      borderRadius: '6px',
                      padding: '4px 10px',
                      cursor: 'pointer',
                      fontSize: '12px',
                    }}
                  >
                    Remove
                  </button>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Add member form */}
        {loggedIn && (
          <form onSubmit={handleAddMember} style={{ display: 'flex', gap: '8px', marginTop: '12px', alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              value={newUsername}
              onInput={(e: any) => setNewUsername(e.target.value)}
              placeholder="Username"
              style={inputStyle}
            />
            <select
              value={newRole}
              onChange={(e: any) => setNewRole(e.target.value)}
              style={{
                background: '#0d1117',
                border: '1px solid #30363d',
                borderRadius: '6px',
                padding: '10px 12px',
                color: '#c9d1d9',
                fontSize: '14px',
              }}
            >
              <option value="member">member</option>
              <option value="owner">owner</option>
            </select>
            <button
              type="submit"
              disabled={addingMember || !newUsername.trim()}
              style={{
                background: '#238636',
                color: '#fff',
                border: 'none',
                borderRadius: '6px',
                padding: '10px 16px',
                cursor: addingMember || !newUsername.trim() ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '14px',
              }}
            >
              Add
            </button>
          </form>
        )}
      </div>

      {/* Danger zone */}
      {loggedIn && (
        <div style={{ border: '1px solid #f85149', borderRadius: '6px', padding: '16px', marginTop: '32px' }}>
          <h3 style={{ color: '#f85149', fontSize: '16px', marginBottom: '12px' }}>Danger zone</h3>
          <p style={{ color: '#8b949e', fontSize: '13px', marginBottom: '12px' }}>
            Once you delete an organization, there is no going back. Please be certain.
          </p>
          <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              value={confirmName}
              onInput={(e: any) => setConfirmName(e.target.value)}
              placeholder={`Type "${org}" to confirm`}
              style={inputStyle}
            />
            <button
              onClick={handleDeleteOrg}
              disabled={deleting || confirmName !== org}
              style={{
                background: confirmName === org ? '#da3633' : '#21262d',
                color: confirmName === org ? '#fff' : '#484f58',
                border: '1px solid #f85149',
                borderRadius: '6px',
                padding: '10px 16px',
                cursor: deleting || confirmName !== org ? 'not-allowed' : 'pointer',
                fontWeight: 'bold',
                fontSize: '14px',
              }}
            >
              {deleting ? 'Deleting...' : 'Delete this organization'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

const inputStyle = {
  background: '#0d1117',
  border: '1px solid #30363d',
  borderRadius: '6px',
  padding: '10px 12px',
  color: '#c9d1d9',
  fontSize: '14px',
};
