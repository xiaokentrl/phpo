// 前端入口：装配 Pinia + Router，挂载根组件
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './styles/base.css'
import './styles/themes/index.css'

createApp(App).use(createPinia()).use(router).mount('#app')
