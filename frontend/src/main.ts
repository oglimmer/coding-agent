import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { applyTheme, getStoredTheme } from './theme'
import './styles.css'

applyTheme(getStoredTheme())

createApp(App).use(createPinia()).use(router).mount('#app')
