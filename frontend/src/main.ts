import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'
import { applyTheme, storedTheme } from './theme'

const deviceDark = typeof window.matchMedia === 'function'
  && window.matchMedia('(prefers-color-scheme: dark)').matches
applyTheme(storedTheme(), deviceDark)
createApp(App).mount('#app')
