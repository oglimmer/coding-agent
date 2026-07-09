import { createRouter, createWebHistory } from 'vue-router'
import { errStatus } from '@/api'
import { useAuthStore } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresAdmin?: boolean
    requiresWrite?: boolean
    hideChrome?: boolean
  }
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/repos' },
    { path: '/login', component: () => import('@/views/LoginView.vue'), meta: { hideChrome: true } },
    {
      path: '/auth/callback',
      component: () => import('@/views/OidcCallbackView.vue'),
      meta: { hideChrome: true },
    },
    {
      path: '/pending',
      component: () => import('@/views/PendingView.vue'),
      meta: { requiresAuth: true, hideChrome: true },
    },
    { path: '/repos', component: () => import('@/views/ReposView.vue'), meta: { requiresAuth: true } },
    { path: '/new', component: () => import('@/views/NewJobView.vue'), meta: { requiresAuth: true, requiresWrite: true } },
    {
      path: '/new/:repoId',
      component: () => import('@/views/NewJobView.vue'),
      props: true,
      meta: { requiresAuth: true, requiresWrite: true },
    },
    { path: '/jobs', component: () => import('@/views/JobsView.vue'), meta: { requiresAuth: true } },
    {
      path: '/jobs/:id',
      component: () => import('@/views/JobDetailView.vue'),
      props: true,
      meta: { requiresAuth: true },
    },
    {
      path: '/admin',
      component: () => import('@/views/AdminView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    { path: '/error', component: () => import('@/views/ErrorView.vue'), props: (r) => ({ code: r.query.code }) },
    { path: '/:pathMatch(.*)*', component: () => import('@/views/ErrorView.vue'), props: { code: '404' } },
  ],
})

// Single guard owns all access control.
router.beforeEach(async (to) => {
  const auth = useAuthStore()

  if (!to.meta.requiresAuth) return true

  if (!auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  try {
    await auth.ensureFreshUser()
  } catch (e) {
    if (errStatus(e) === 401) {
      auth.clear()
      return { path: '/login', query: { redirect: to.fullPath } }
    }
    return { path: '/error', query: { code: '500' } }
  }

  // Viewers (review-only) have no access yet: hold them on the pending screen
  // and keep everyone else off it.
  if (auth.isPending) {
    return to.path === '/pending' ? true : { path: '/pending' }
  }
  if (to.path === '/pending') {
    return { path: '/repos' }
  }

  if (to.meta.requiresAdmin && !auth.isAdmin) {
    return { path: '/error', query: { code: '403' } }
  }
  if (to.meta.requiresWrite && !auth.canWrite) {
    return { path: '/error', query: { code: '403' } }
  }
  return true
})

export default router
