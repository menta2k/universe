<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useTheme } from 'vuetify'

import { useAuthStore } from '../stores/auth'

interface NavItem {
  readonly title: string
  readonly icon: string
  readonly to: string
}

const navItems: readonly NavItem[] = [
  { title: 'Dashboard', icon: 'mdi-view-dashboard-outline', to: '/' },
  { title: 'Machines', icon: 'mdi-server', to: '/machines' },
  { title: 'Profiles', icon: 'mdi-file-cog-outline', to: '/profiles' },
  { title: 'DHCP', icon: 'mdi-ip-network-outline', to: '/dhcp' },
  { title: 'Boot Files', icon: 'mdi-file-download-outline', to: '/bootfiles' },
  { title: 'Sessions', icon: 'mdi-progress-clock', to: '/sessions' },
]

const drawer = ref(true)
const auth = useAuthStore()
const router = useRouter()
const theme = useTheme()

function toggleTheme(): void {
  theme.global.name.value = theme.global.current.value.dark ? 'light' : 'dark'
}

async function onLogout(): Promise<void> {
  await auth.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <v-navigation-drawer v-model="drawer">
    <v-list-item
      class="py-3"
      prepend-icon="mdi-lan-connect"
      title="Netboot Manager"
      subtitle="Provisioning console"
    />
    <v-divider />
    <v-list density="comfortable" nav>
      <v-list-item
        v-for="item in navItems"
        :key="item.to"
        :prepend-icon="item.icon"
        :title="item.title"
        :to="item.to"
        exact
        color="primary"
      />
    </v-list>
  </v-navigation-drawer>

  <v-app-bar flat border="b">
    <v-app-bar-nav-icon @click="drawer = !drawer" />
    <v-app-bar-title>Netboot Manager</v-app-bar-title>
    <v-spacer />
    <v-btn icon="mdi-theme-light-dark" variant="text" @click="toggleTheme" />
    <v-chip class="mx-2" prepend-icon="mdi-account-circle-outline" variant="tonal">
      {{ auth.displayName }}
    </v-chip>
    <v-btn prepend-icon="mdi-logout" variant="text" @click="onLogout"> Logout </v-btn>
  </v-app-bar>

  <v-main>
    <v-container fluid class="pa-6">
      <router-view />
    </v-container>
  </v-main>
</template>
