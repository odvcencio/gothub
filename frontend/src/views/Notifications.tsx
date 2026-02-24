import { useEffect, useState } from 'preact/hooks';
import { listNotifications, markNotificationRead, markAllNotificationsRead, getToken } from '../api/client';

interface Props {
  path?: string;
}

export function NotificationsView({ path }: Props) {
  const [notifications, setNotifications] = useState<any[]>([]);
  const [filter, setFilter] = useState<'unread' | 'all'>('unread');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const loggedIn = !!getToken();

  const loadNotifications = () => {
    setLoading(true);
    listNotifications(filter === 'unread' ? true : undefined)
      .then(setNotifications)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (!loggedIn) return;
    loadNotifications();
  }, [filter]);

  const onMarkRead = async (id: number) => {
    try {
      await markNotificationRead(id);
      setNotifications(notifications.map((n) =>
        n.id === id ? { ...n, read_at: new Date().toISOString() } : n
      ));
    } catch (err: any) {
      setError(err.message || 'failed to mark notification as read');
    }
  };

  const onMarkAllRead = async () => {
    try {
      await markAllNotificationsRead();
      setNotifications(notifications.map((n) => ({ ...n, read_at: n.read_at || new Date().toISOString() })));
    } catch (err: any) {
      setError(err.message || 'failed to mark all notifications as read');
    }
  };

  if (!loggedIn) {
    return (
      <div style={{ color: '#8b949e', padding: '40px', textAlign: 'center', border: '1px solid #30363d', borderRadius: '6px' }}>
        Sign in to view notifications
      </div>
    );
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', flexWrap: 'wrap', marginBottom: '16px' }}>
        <h1 style={{ fontSize: '20px', color: '#f0f6fc', margin: 0 }}>Notifications</h1>
        {filter === 'unread' && notifications.some((n) => !n.read_at) && (
          <button
            onClick={onMarkAllRead}
            style={{ background: '#238636', color: '#fff', border: 'none', borderRadius: '6px', padding: '8px 12px', cursor: 'pointer', fontSize: '13px' }}
          >
            Mark all as read
          </button>
        )}
      </div>

      <div style={{ display: 'flex', gap: '8px', marginBottom: '12px' }}>
        {(['unread', 'all'] as const).map((s) => (
          <button
            key={s}
            onClick={() => setFilter(s)}
            style={{
              background: filter === s ? '#1f6feb' : '#161b22',
              color: '#c9d1d9',
              border: '1px solid #30363d',
              borderRadius: '6px',
              padding: '6px 10px',
              cursor: 'pointer',
              fontSize: '13px',
            }}
          >
            {s.charAt(0).toUpperCase() + s.slice(1)}
          </button>
        ))}
      </div>

      {error && <div style={{ color: '#f85149', marginBottom: '12px' }}>{error}</div>}

      {loading ? (
        <div style={{ color: '#8b949e' }}>Loading...</div>
      ) : notifications.length === 0 ? (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px', padding: '16px', color: '#8b949e' }}>No notifications</div>
      ) : (
        <div style={{ border: '1px solid #30363d', borderRadius: '6px' }}>
          {notifications.map((notification, idx) => {
            const isUnread = !notification.read_at;
            return (
              <div
                key={notification.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: '12px',
                  padding: '12px 14px',
                  borderTop: idx === 0 ? 'none' : '1px solid #30363d',
                  borderLeft: isUnread ? '3px solid #58a6ff' : '3px solid transparent',
                }}
              >
                <a
                  href={notification.resource_path}
                  style={{ textDecoration: 'none', flex: 1, minWidth: 0 }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
                    <span style={{
                      color: '#fff',
                      background: typeBadgeColor(notification.type),
                      borderRadius: '12px',
                      padding: '2px 8px',
                      fontWeight: 'bold',
                      fontSize: '11px',
                      whiteSpace: 'nowrap',
                    }}>
                      {notification.type}
                    </span>
                    <span style={{ color: '#f0f6fc', fontSize: '14px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {notification.title}
                    </span>
                  </div>
                  <div style={{ color: '#8b949e', fontSize: '12px' }}>
                    {notification.actor_name} &middot; {formatRelativeTime(notification.created_at)}
                  </div>
                </a>
                {isUnread && (
                  <button
                    onClick={(e: Event) => { e.preventDefault(); onMarkRead(notification.id); }}
                    style={{
                      border: '1px solid #30363d',
                      background: '#161b22',
                      color: '#c9d1d9',
                      borderRadius: '6px',
                      padding: '5px 9px',
                      cursor: 'pointer',
                      fontSize: '12px',
                      whiteSpace: 'nowrap',
                      flexShrink: 0,
                    }}
                  >
                    Mark as read
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function typeBadgeColor(type: string): string {
  switch (type) {
    case 'issue': return '#238636';
    case 'pull_request': return '#8957e5';
    case 'review': return '#d29922';
    case 'comment': return '#30363d';
    case 'mention': return '#1f6feb';
    default: return '#30363d';
  }
}

function formatRelativeTime(ts: unknown): string {
  if (!ts) return '';
  const d = typeof ts === 'string' ? new Date(ts) : new Date(Number(ts));
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const diff = now.getTime() - d.getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`;
  const days = Math.floor(hours / 24);
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return d.toLocaleDateString();
}
