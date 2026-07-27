import { createApp } from 'vue';
import App from './App.vue';
import './style.css';
import { registerServiceWorker } from './pwa';

createApp(App).mount('#app');

// ホーム画面・デスクトップへ入れられるようにする。画面の描画を待たせないよう
// マウントの後に登録する。
registerServiceWorker();
