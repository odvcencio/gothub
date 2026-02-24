import { useEffect, useState } from 'preact/hooks';
import { changePassword, getAuthCapabilities, getToken } from '../api/client';

interface Props {
  path?: string;
}

export function ChangePasswordView({ path }: Props) {
  const loggedIn = !!getToken();
  const [passwordAuthEnabled, setPasswordAuthEnabled] = useState<boolean | null>(null);

  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!loggedIn) return;
    getAuthCapabilities()
      .then((caps) => setPasswordAuthEnabled(!!caps.password_auth_enabled))
      .catch(() => setPasswordAuthEnabled(false));
  }, [loggedIn]);

  if (!loggedIn) {
    return (
      <div style={{ maxWidth: '600px', margin: '60px auto', textAlign: 'center' }}>
        <p style={{ color: '#8b949e', fontSize: '16px' }}>Sign in to change your password</p>
      </div>
    );
  }

  if (passwordAuthEnabled === false) {
    return (
      <div style={{ maxWidth: '600px', margin: '60px auto', textAlign: 'center' }}>
        <p style={{ color: '#8b949e', fontSize: '16px' }}>
          Password auth is disabled on this instance.
        </p>
      </div>
    );
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault();
    setError('');
    setSuccess('');

    if (newPassword.length < 8) {
      setError('New password must be at least 8 characters');
      return;
    }

    if (newPassword !== confirmPassword) {
      setError('New passwords do not match');
      return;
    }

    setSubmitting(true);
    try {
      await changePassword(currentPassword, newPassword);
      setSuccess('Password changed successfully');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: any) {
      setError(err.message || 'Failed to change password');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ maxWidth: '600px', margin: '0 auto' }}>
      <div style={{ marginBottom: '16px' }}>
        <a href="/settings" style={{ color: '#58a6ff', fontSize: '14px', textDecoration: 'none' }}>
          &larr; Back to settings
        </a>
      </div>

      <h1 style={{ fontSize: '24px', color: '#f0f6fc', marginBottom: '24px' }}>Change password</h1>

      {error && (
        <div style={{ color: '#f85149', marginBottom: '16px', padding: '12px', background: '#1c1214', border: '1px solid #f85149', borderRadius: '6px' }}>
          {error}
        </div>
      )}

      {success && (
        <div style={{ color: '#238636', marginBottom: '16px', padding: '12px', background: '#12261e', border: '1px solid #238636', borderRadius: '6px' }}>
          {success}
        </div>
      )}

      <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '24px', background: '#161b22' }}>
        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '14px', marginBottom: '8px' }}>
              Current password
            </label>
            <input
              type="password"
              value={currentPassword}
              onInput={(e: any) => setCurrentPassword(e.target.value)}
              placeholder="Enter current password"
              style={inputStyle}
            />
          </div>

          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '14px', marginBottom: '8px' }}>
              New password
            </label>
            <input
              type="password"
              value={newPassword}
              onInput={(e: any) => setNewPassword(e.target.value)}
              placeholder="Enter new password (min 8 characters)"
              style={inputStyle}
            />
          </div>

          <div>
            <label style={{ display: 'block', color: '#c9d1d9', fontSize: '14px', marginBottom: '8px' }}>
              Confirm new password
            </label>
            <input
              type="password"
              value={confirmPassword}
              onInput={(e: any) => setConfirmPassword(e.target.value)}
              placeholder="Confirm new password"
              style={inputStyle}
            />
          </div>

          <button
            type="submit"
            disabled={submitting || !currentPassword || !newPassword || !confirmPassword}
            style={{
              background: '#238636',
              color: '#fff',
              border: 'none',
              padding: '10px 16px',
              borderRadius: '6px',
              cursor: submitting ? 'not-allowed' : 'pointer',
              fontWeight: 'bold',
              fontSize: '14px',
              alignSelf: 'flex-start',
              opacity: submitting || !currentPassword || !newPassword || !confirmPassword ? 0.6 : 1,
            }}
          >
            {submitting ? 'Changing password...' : 'Change password'}
          </button>
        </form>
      </div>
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
  width: '100%',
  boxSizing: 'border-box' as const,
};
