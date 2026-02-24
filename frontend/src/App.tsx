import Router from 'preact-router';
import { Header } from './components/Header';
import { Home } from './views/Home';
import { RepoView } from './views/Repo';
import { CodeView } from './views/Code';
import { DiffView } from './views/Diff';
import { PRListView } from './views/PRList';
import { PRDetailView } from './views/PRDetail';
import { PRCreateView } from './views/PRCreate';
import { CommitsView } from './views/Commits';
import { IssueListView } from './views/IssueList';
import { IssueDetailView } from './views/IssueDetail';
import { NotificationsView } from './views/Notifications';
import { SettingsView } from './views/Settings';
import { RepoSettingsView } from './views/RepoSettings';
import { OrgDetailView } from './views/OrgView';
import { SymbolSearchView } from './views/SymbolSearch';
import { ReferencesView } from './views/References';
import { CallGraphView } from './views/CallGraph';
import { EntityHistoryView } from './views/EntityHistory';
import { NotFoundView } from './views/NotFound';
import { ChangePasswordView } from './views/ChangePassword';

export function App() {
  return (
    <div>
      <Header />
      <main style={{ maxWidth: '1200px', margin: '0 auto', padding: '20px' }}>
        <Router>
          <Home path="/" />
          <NotificationsView path="/notifications" />
          <SettingsView path="/settings" />
          <ChangePasswordView path="/settings/password" />
          <OrgDetailView path="/orgs/:org" />
          <RepoSettingsView path="/:owner/:repo/settings" />
          <CodeView path="/:owner/:repo/tree/:ref/:path*" />
          <CodeView path="/:owner/:repo/blob/:ref/:path*" />
          <CommitsView path="/:owner/:repo/commits/:ref" />
          <DiffView path="/:owner/:repo/diff/:spec" />
          <SymbolSearchView path="/:owner/:repo/symbols/:ref" />
          <ReferencesView path="/:owner/:repo/references/:ref" />
          <CallGraphView path="/:owner/:repo/callgraph/:ref" />
          <EntityHistoryView path="/:owner/:repo/entity-history/:ref" />
          <PRListView path="/:owner/:repo/pulls" />
          <PRCreateView path="/:owner/:repo/pulls/new" />
          <PRDetailView path="/:owner/:repo/pulls/:number" />
          <IssueListView path="/:owner/:repo/issues" />
          <IssueDetailView path="/:owner/:repo/issues/:number" />
          <RepoView path="/:owner/:repo" />
          <NotFoundView default />
        </Router>
      </main>
    </div>
  );
}
