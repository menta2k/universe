import { createRouter, createWebHistory } from 'vue-router'

import MainLayout from '../layouts/MainLayout.vue'
import { useAuthStore } from '../stores/auth'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../pages/LoginPage.vue'),
    },
    {
      path: '/',
      component: MainLayout,
      children: [
        {
          path: '',
          name: 'dashboard',
          component: () => import('../pages/DashboardPage.vue'),
        },
        {
          path: 'machines',
          name: 'machines',
          component: () => import('../pages/MachinesPage.vue'),
        },
        {
          path: 'profiles',
          name: 'profiles',
          component: () => import('../pages/ProfilesPage.vue'),
        },
        {
          path: 'dhcp',
          name: 'dhcp',
          component: () => import('../pages/DhcpPage.vue'),
        },
        {
          path: 'bootfiles',
          name: 'bootfiles',
          component: () => import('../pages/BootFilesPage.vue'),
        },
        {
          path: 'sessions',
          name: 'sessions',
          component: () => import('../pages/SessionsPage.vue'),
        },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.checked) {
    await auth.fetchMe()
  }
  if (to.name !== 'login' && !auth.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && auth.isAuthenticated) {
    return { name: 'dashboard' }
  }
  return true
})
