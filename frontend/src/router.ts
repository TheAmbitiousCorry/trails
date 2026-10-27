import { createRouter, createWebHistory } from 'vue-router'
import { projectId } from './api'
import ProjectsPage from './pages/ProjectsPage.vue'
import MapPage from './pages/MapPage.vue'
import SignPage from './pages/SignPage.vue'
import EntryPage from './pages/EntryPage.vue'
import SessionsPage from './pages/SessionsPage.vue'
import SessionPage from './pages/SessionPage.vue'
import GapsPage from './pages/GapsPage.vue'
import ProvisionalPage from './pages/ProvisionalPage.vue'
import TagsPage from './pages/TagsPage.vue'
import LogsPage from './pages/LogsPage.vue'
import GraphPage from './pages/GraphPage.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'projects', component: ProjectsPage },
    { path: '/p/:project', name: 'map', component: MapPage },
    { path: '/p/:project/signs/:id', name: 'sign', component: SignPage },
    { path: '/p/:project/entries/:id', name: 'entry', component: EntryPage },
    { path: '/p/:project/graph', name: 'graph', component: GraphPage },
    { path: '/p/:project/sessions', name: 'sessions', component: SessionsPage },
    { path: '/p/:project/sessions/:id', name: 'session', component: SessionPage },
    { path: '/p/:project/gaps', name: 'gaps', component: GapsPage },
    { path: '/p/:project/provisional', name: 'provisional', component: ProvisionalPage },
    { path: '/p/:project/tags', name: 'tags', component: TagsPage },
    { path: '/p/:project/logs', name: 'logs', component: LogsPage },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

// Set once the route has changed and before it renders: earlier, the shell would still resolve its links against the old route.
router.afterEach((to) => {
  projectId.value = String(to.params.project ?? '')
})
