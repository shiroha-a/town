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
    // 画面側のヘッダ。APIはGo側で同等のものを付けている。
    // CSPは画像だけ外部(Misskeyのアイコン・カスタム絵文字)を許す。
    headers: {
      'X-Content-Type-Options': 'nosniff',
      'Referrer-Policy': 'no-referrer',
      'Content-Security-Policy': [
        "default-src 'self'",
        "img-src 'self' https: data:",
        "style-src 'self' 'unsafe-inline'",
        // Viteの開発サーバはHMRでインラインスクリプトとevalを使う。
        "script-src 'self' 'unsafe-inline' 'unsafe-eval'",
        "connect-src 'self' ws: wss:",
        "frame-ancestors 'none'",
      ].join('; '),
    },
    host: '0.0.0.0',
    port: 5173,
    allowedHosts,
    proxy: {
      '/api': {
        target: process.env.TOWN_API_TARGET ?? 'http://localhost:8090',
        changeOrigin: true,
        // クライアントIPをX-Forwarded-Forで渡す。これが無いとAPI側からは
        // 全アクセスがこのプロキシからに見え、IP単位のレート制限が効かない。
        xfwd: true,
      },
    },
  },
});
