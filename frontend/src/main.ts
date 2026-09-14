import 'shared-frontend/src/styles/tokens.css'
import { createApp } from 'vue'
import './style.css'
import 'shared-frontend/src/styles/transitions.css'
import 'shared-frontend/src/styles/toolbar.css'
import { initTheme } from 'shared-frontend'
import App from './App.vue'

initTheme()
createApp(App).mount('#app')
