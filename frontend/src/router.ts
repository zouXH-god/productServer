import { createRouter, createWebHistory } from 'vue-router'
import { session } from './api'
import LoginView from './views/LoginView.vue'
import ProjectsView from './views/ProjectsView.vue'
import ProjectView from './views/ProjectView.vue'
import WorkflowsView from './views/WorkflowsView.vue'
import SSHView from './views/SSHView.vue'
import DashboardView from './views/DashboardView.vue'
import RunsView from './views/RunsView.vue'
import WorkflowHubView from './views/WorkflowHubView.vue'
import EnvironmentView from './views/EnvironmentView.vue'
import AIProvidersView from './views/AIProvidersView.vue'
import AccountView from './views/AccountView.vue'
import UsersView from './views/UsersView.vue'

export const router = createRouter({ history: createWebHistory(), routes: [
  { path: '/login', component: LoginView },
  { path: '/', component: DashboardView, meta: { auth: true,title:'概览' } },
  { path: '/projects', component: ProjectsView, meta: { auth: true,title:'项目' } },
  { path: '/projects/:id', component: ProjectView, meta: { auth: true,title:'项目详情' } },
  { path: '/workflows/editor/:id', component: WorkflowsView, meta: { auth: true,title:'工作流编辑器' } },
  { path: '/workflows', component: WorkflowHubView, meta: { auth: true,title:'工作流' } },
  { path: '/runs', component: RunsView, meta: { auth: true,title:'运行记录' } },
  { path: '/settings/ssh', component: SSHView, meta: { auth: true,title:'SSH 资源' } },
  { path: '/settings/environment', component: EnvironmentView, meta: { auth: true,title:'环境变量' } },
  { path: '/settings/ai', component: AIProvidersView, meta: { auth: true,title:'AI 模型' } },
  { path: '/settings/account', component: AccountView, meta: { auth: true,title:'个人设置' } },
  { path: '/admin/users', component: UsersView, meta: { auth: true,title:'用户管理' } }
] })
router.beforeEach(to => { if (to.meta.auth && !session.token) return '/login'; if (to.path === '/login' && session.token) return '/' })
