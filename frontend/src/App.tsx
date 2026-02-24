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

export function App() {
  return (
    <div>
      <Header />
      <main style={{ maxWidth: '1200px', margin: '0 auto', padding: '20px' }}>
        <Router>
          <Home path="/" />
          <RepoView path="/:owner/:repo" />
          <CodeView path="/:owner/:repo/tree/:ref/:path*" />
          <CodeView path="/:owner/:repo/blob/:ref/:path*" />
          <CommitsView path="/:owner/:repo/commits/:ref" />
          <DiffView path="/:owner/:repo/diff/:spec" />
          <PRListView path="/:owner/:repo/pulls" />
          <PRCreateView path="/:owner/:repo/pulls/new" />
          <PRDetailView path="/:owner/:repo/pulls/:number" />
          <IssueListView path="/:owner/:repo/issues" />
          <IssueDetailView path="/:owner/:repo/issues/:number" />
        </Router>
      </main>
    </div>
  );
}
