import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// 開発時はViteのdevサーバが /api をGoバックエンド(:8090)へプロキシする。
// これによりブラウザからは同一オリジンに見え、CORS設定が不要になる。

// Viteはlocalhost/IP以外のホスト名を既定で弾く。Tailscale等のホスト名で
// アクセスする場合は TOWN_ALLOWED_HOSTS=".ts.net,foo.example" のように許可する
// (先頭ドットでサブドメインを許可)。TOWN_ALLOWED_HOSTS=all で全ホスト許可
// (Docker常時テストサーバー向け)。未設定ならViteの既定(localhost/IP)。
const allowedHostsEnv = process.env.TOWN_ALLOWED_HOSTS;
const allowedHosts =
  allowedHostsEnv === 'all'
    ? true
    : allowedHostsEnv
      ? allowedHostsEnv.split(',').map((h) => h.trim())
      : undefined;

export default defineConfig({
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    allowedHosts,
    proxy: {
      '/api': {
        target: process.env.TOWN_API_TARGET ?? 'http://localhost:8090',
        changeOrigin: true,
      },
    },
  },
});
