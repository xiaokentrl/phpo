// 前端入口：装配 Pinia + Router，挂载根组件
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './styles/base.css'
import './styles/themes/index.css'

// 禁掉 WebView 内核的默认右键菜单（Linux WebKitGTK / Windows WebView2 会弹「重新加载/检查元素」等），
// 桌面应用语境下它只是把浏览器内核露出来。元素上自带的 @contextmenu.prevent 处理器（如站点路径右键复制）不受影响。
document.addEventListener('contextmenu', (e) => e.preventDefault())

createApp(App).use(createPinia()).use(router).mount('#app')
