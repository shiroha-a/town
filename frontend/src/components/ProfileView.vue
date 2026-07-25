<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, type Player, type PublicSummary, type MisskeyProfileResp } from '../api';
import RichText from './RichText.vue';

// prof施設。役場の住民名鑑がゲーム内ステータスを見る場所なのに対し、ここは
// 住民のMisskey側の顔を見る場所。ゲーム内からリモートフォローもできる。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const roster = ref<PublicSummary[]>([]);
const selectedId = ref(props.player.id);
const data = ref<MisskeyProfileResp | null>(null);
const message = ref('');
const loading = ref(false);
const busy = ref(false);

const isSelf = computed(() => selectedId.value === props.player.id);
const prof = computed(() => data.value?.profile ?? null);
const follow = computed(() => data.value?.follow ?? null);
// 表示名はMisskeyの名前を優先し、未設定ならusername。
const shownName = computed(() => prof.value?.name?.trim() || prof.value?.username || '');

function fail(e: unknown) {
  message.value = e instanceof Error ? e.message : String(e);
}

async function select(id: number) {
  selectedId.value = id;
  data.value = null;
  message.value = '';
  loading.value = true;
  try {
    data.value = await api.misskeyProfile(id);
  } catch (e) {
    fail(e);
  } finally {
    loading.value = false;
  }
}

async function doFollow() {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.misskeyFollow(selectedId.value);
    message.value = res.message;
    if (data.value?.follow) {
      data.value.follow.following = res.following;
      data.value.follow.pending = res.pending;
      data.value.follow.known = res.following || res.pending;
      if (res.fallback_url) data.value.follow.fallback_url = res.fallback_url;
    }
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

async function doUnfollow() {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    const res = await api.misskeyUnfollow(selectedId.value);
    message.value = res.message;
    if (data.value?.follow) {
      data.value.follow.following = res.following;
      data.value.follow.pending = false;
    }
  } catch (e) {
    fail(e);
  } finally {
    busy.value = false;
  }
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}/${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

onMounted(async () => {
  try {
    roster.value = await api.listPlayers();
  } catch (e) {
    fail(e);
  }
  await select(props.player.id);
});
</script>

<template>
  <div class="facility-page prof-page">
    <button class="btn back" @click="emit('back')">街に戻る</button>
    <div class="prof-header">
      <div class="lead">
        プロフィールです。住民のMisskeyアカウントを見ることができます。<br />
        気になる住民はこの場からフォローできます。
      </div>
      <div class="title">プロフィール</div>
    </div>

    <div v-if="message" class="message" :class="{ error: !data }">{{ message }}</div>

    <div class="prof-layout">
      <div class="roster">
        <div class="roster-head">住民一覧({{ roster.length }}人)</div>
        <button
          v-for="m in roster"
          :key="m.id"
          class="roster-item"
          :class="{ active: m.id === selectedId }"
          @click="select(m.id)"
        >
          {{ m.display_name }}
          <span class="rself" v-if="m.id === player.id">（あなた）</span>
        </button>
      </div>

      <div class="card">
        <div v-if="loading" class="loading">読み込んでいます…</div>

        <template v-else-if="prof">
          <div
            class="banner"
            :class="{ none: !prof.banner_url }"
            :style="prof.banner_url ? { backgroundImage: `url(${prof.banner_url})` } : {}"
          ></div>
          <div class="identity">
            <img v-if="prof.avatar_url" class="avatar" :src="prof.avatar_url" alt="" />
            <div class="names">
              <div class="dname">
                <RichText :text="shownName" :emojis="prof.emojis" />
                <span class="badge" v-if="prof.is_bot">Bot</span>
                <span class="badge" v-if="prof.is_locked">承認制</span>
              </div>
              <div class="acct">{{ prof.acct }}</div>
              <div class="ingame">街での名前: {{ data?.display_name }}</div>
            </div>
          </div>

          <!-- MFMは描画せずそのまま文字として出す(表示崩れとXSSを避ける) -->
          <div class="desc" v-if="prof.description">
            <RichText :text="prof.description" :emojis="prof.emojis" />
          </div>
          <div class="desc empty" v-else>自己紹介はありません。</div>

          <div class="counts">
            <span><b>{{ prof.notes_count }}</b> 投稿</span>
            <span><b>{{ prof.following_count }}</b> フォロー</span>
            <span><b>{{ prof.followers_count }}</b> フォロワー</span>
          </div>

          <div class="actions">
            <template v-if="!isSelf && follow">
              <button
                v-if="follow.following"
                class="btn"
                :disabled="busy"
                data-test="unfollow"
                @click="doUnfollow"
              >
                フォロー中（解除する）
              </button>
              <span v-else-if="follow.pending" class="pending">フォロー申請中</span>
              <button
                v-else-if="follow.can_follow"
                class="btn primary"
                :disabled="busy"
                data-test="follow"
                @click="doFollow"
              >
                {{ prof.is_locked ? 'フォロー申請する' : 'フォローする' }}
              </button>
              <a
                v-if="!follow.following && follow.fallback_url"
                class="btn link"
                :href="follow.fallback_url"
                target="_blank"
                rel="noopener noreferrer"
              >
                Misskeyでフォロー
              </a>
            </template>
            <a class="btn link" :href="prof.profile_url" target="_blank" rel="noopener noreferrer">
              Misskeyで開く
            </a>
          </div>

          <div class="fetched">
            {{ fmtTime(prof.fetched_at) }}時点の情報
            <span class="stale" v-if="prof.stale">（インスタンスに接続できず、以前の内容を表示しています）</span>
          </div>
        </template>

        <div v-else class="loading">プロフィールを表示できません。</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.prof-page {
  background-color: #e8dcc0;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.prof-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.prof-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.prof-header .title {
  flex: 0 0 140px;
  background: #997a44;
  color: #fff;
  font-weight: bold;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.prof-layout {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.roster {
  flex: 0 0 180px;
  background: #fff;
  border: 1px solid #999;
  max-height: 70vh;
  overflow-y: auto;
}
.roster-head {
  background: #997a44;
  color: #fff;
  font-size: 12px;
  padding: 4px 8px;
  position: sticky;
  top: 0;
}
.roster-item {
  display: block;
  width: 100%;
  text-align: left;
  border: 0;
  border-bottom: 1px dotted #ccc;
  background: none;
  font-size: 13px;
  padding: 5px 8px;
  cursor: pointer;
  color: #006699;
}
.roster-item:hover {
  background: #f5f0e0;
}
.roster-item.active {
  background: #ffeecc;
  font-weight: bold;
}
.rself {
  color: #996600;
  font-size: 11px;
}
.card {
  flex: 1 1 auto;
  background: #fff;
  border: 1px solid #999;
  min-height: 200px;
  overflow: hidden;
}
.loading {
  padding: 24px;
  color: #666;
  font-size: 13px;
  text-align: center;
}
.banner {
  height: 120px;
  background: #d8cba8 center/cover no-repeat;
}
/* バナー未設定のときに大きな余白を作らない */
.banner.none {
  height: 52px;
}
.identity {
  display: flex;
  gap: 12px;
  padding: 0 14px;
  margin-top: -32px;
}
.avatar {
  width: 72px;
  height: 72px;
  border-radius: 8px;
  border: 3px solid #fff;
  background: #eee;
  object-fit: cover;
}
.names {
  padding-top: 36px;
  min-width: 0;
}
.dname {
  font-size: 16px;
  font-weight: bold;
  color: #333;
  word-break: break-word;
}
.badge {
  font-size: 10px;
  font-weight: normal;
  background: #997a44;
  color: #fff;
  border-radius: 3px;
  padding: 1px 5px;
  margin-left: 6px;
  vertical-align: middle;
}
.acct {
  font-size: 12px;
  color: #666;
  word-break: break-all;
}
.ingame {
  font-size: 11px;
  color: #996600;
  margin-top: 2px;
}
.desc {
  margin: 12px 14px;
  padding: 8px 10px;
  background: #f8f5ec;
  border: 1px solid #e0d8c0;
  font-size: 13px;
  line-height: 1.7;
  color: #333;
  white-space: pre-wrap;
  word-break: break-word;
}
.desc.empty {
  color: #999;
}
.counts {
  display: flex;
  gap: 18px;
  margin: 0 14px 12px;
  font-size: 13px;
  color: #333;
}
.counts b {
  color: #663300;
  font-size: 15px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin: 0 14px 12px;
}
.btn.link {
  text-decoration: none;
  display: inline-block;
}
.pending {
  font-size: 13px;
  color: #996600;
}
.fetched {
  border-top: 1px dotted #ccc;
  padding: 6px 14px;
  font-size: 11px;
  color: #999;
}
.stale {
  color: #cc6600;
}
.message {
  margin-bottom: 8px;
}
/* モバイル: 一覧とカードを縦に積む */
@media (max-width: 700px) {
  .prof-layout {
    flex-direction: column;
  }
  .roster {
    flex: 1 1 auto;
    width: 100%;
    max-height: 200px;
  }
  .prof-header .title {
    flex-basis: 90px;
    font-size: 14px;
  }
}
</style>
