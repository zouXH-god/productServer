import { createRouter, createWebHistory } from 'vue-router'
import { session } from './api'
import LoginView from './views/LoginView.vue'
import ProjectsView from './views/ProjectsView.vue'
import ProjectView from './views/ProjectView.vue'
import WorkflowsView from './views/WorkflowsView.vue'
import SSHView from './views/SSHView.vue'

export const router = createRouter({ history: createWebHistory(), routes: [
  { path: '/login', component: LoginView },
  { path: '/', component: ProjectsView, meta: { auth: true } },
  { path: '/projects/:id', component: ProjectView, meta: { auth: true } },
  { path: '/projects/:id/workflows', component: WorkflowsView, meta: { auth: true } },
  { path: '/settings/ssh', component: SSHView, meta: { auth: true } }
] })
router.beforeEach(to => { if (to.meta.auth && !session.token) return '/login'; if (to.path === '/login' && session.token) return '/' })
