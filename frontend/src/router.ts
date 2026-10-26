import { createRouter, createWebHistory } from 'vue-router'
import MapPage from './pages/MapPage.vue'
import SignPage from './pages/SignPage.vue'
import EntryPage from './pages/EntryPage.vue'
import SessionsPage from './pages/SessionsPage.vue'
import SessionPage from './pages/SessionPage.vue'
import GapsPage from './pages/GapsPage.vue'
import ProvisionalPage from './pages/ProvisionalPage.vue'
import TagsPage from './pages/TagsPage.vue'
import LogsPage from './pages/LogsPage.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'map', component: MapPage },
    { path: '/signs/:id', name: 'sign', component: SignPage },
    { path: '/entries/:id', name: 'entry', component: EntryPage },
    { path: '/sessions', name: 'sessions', component: SessionsPage },
    { path: '/sessions/:id', name: 'session', component: SessionPage },
    { path: '/gaps', name: 'gaps', component: GapsPage },
    { path: '/provisional', name: 'provisional', component: ProvisionalPage },
    { path: '/tags', name: 'tags', component: TagsPage },
    { path: '/logs', name: 'logs', component: LogsPage },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})
