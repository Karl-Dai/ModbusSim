import 'shared-frontend/src/styles/tokens.css'
import 'shared-frontend/src/styles/toolbar.css'
import { initTheme } from 'shared-frontend'
import { createApp } from 'vue'
import App from './App.vue'

initTheme()
createApp(App).mount('#app')
