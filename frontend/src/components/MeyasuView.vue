<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import {
  api,
  FEEDBACK_KINDS,
  FEEDBACK_STATUSES,
  type Player,
  type FeedbackPost,
  type FeedbackDetail,
} from '../api';
import Toast from './Toast.vue';
import { useToast } from '../toast';

// 目安箱: 不具合・要望・質問の投稿所。GitHub issueのうち、この規模で効く要素
// (種別・状態・コメント・賛同)だけを持つ。状態を動かせるのは運営だけ。
const props = defineProps<{ player: Player }>();
const emit = defineEmits<{ back: [] }>();

const isAdmin = computed(() => props.player.roles.includes('admin'));
const { toast, showToast, closeToast } = useToast();
const message = ref('');
const busy = ref(false);

const posts = ref<FeedbackPost[]>([]);
const detail = ref<FeedbackDetail | null>(null);
// 絞り込みと並び。空文字=すべて。
const filterKind = ref('');
const filterStatus = ref('');
const sort = ref<'new' | 'votes'>('new');

// 投稿フォーム。
const formOpen = ref(false);
const draft = ref({ kind: 'bug', title: '', body: '' });
const commentDraft = ref('');

const fmtDate = (iso: string) => {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(
    d.getMinutes(),
  ).padStart(2, '0')}`;
};

async function loadList() {
  try {
    posts.value = await api.feedbackList(filterKind.value, filterStatus.value, sort.value);
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  }
}
onMounted(loadList);

async function open(pid: number) {
  busy.value = true;
  try {
    detail.value = await api.feedbackGet(pid);
    commentDraft.value = '';
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e);
  } finally {
    busy.value = false;
  }
}
function closeDetail() {
  detail.value = null;
  void loadList();
}

/** 一覧・詳細のどちらから操作しても、両方の表示を合わせ直す。 */
async function run(fn: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true;
  message.value = '';
  try {
    await fn();
    await loadList();
  } catch (e) {
    showToast({
      variant: 'error',
      title: 'できませんでした',
      lines: [e instanceof Error ? e.message : String(e)],
      icon: 'item',
    });
  } finally {
    busy.value = false;
  }
}

async function submit() {
  await run(async () => {
    const d = await api.feedbackCreate(
      props.player.id,
      draft.value.kind,
      draft.value.title,
      draft.value.body,
    );
    detail.value = d;
    formOpen.value = false;
    draft.value = { kind: 'bug', title: '', body: '' };
    showToast({ variant: 'item', title: '投稿しました', lines: [], icon: 'item' });
  });
}

async function comment() {
  const d = detail.value;
  if (!d || !commentDraft.value.trim()) return;
  await run(async () => {
    detail.value = await api.feedbackComment(props.player.id, d.post.id, commentDraft.value);
    commentDraft.value = '';
  });
}

async function vote(pid: number) {
  await run(async () => {
    const d = await api.feedbackVote(props.player.id, pid);
    if (detail.value) detail.value = d;
  });
}

async function setStatus(pid: number, status: string) {
  await run(async () => {
    detail.value = await api.adminFeedbackStatus(pid, status);
  });
}

async function removePost(pid: number) {
  await run(async () => {
    await api.feedbackDelete(props.player.id, pid);
    detail.value = null;
  });
}

async function removeComment(cid: number) {
  const d = detail.value;
  if (!d) return;
  await run(async () => {
    await api.feedbackDeleteComment(props.player.id, cid);
    detail.value = await api.feedbackGet(d.post.id);
  });
}

const canDelete = (authorID: number | null) => isAdmin.value || authorID === props.player.id;
</script>

<template>
  <div class="facility-page my-page">
    <Toast :toast="toast" @close="closeToast" />
    <button class="btn back" @click="emit('back')">街に戻る</button>

    <div class="my-header">
      <div class="lead">
        街の不具合や「こうしてほしい」を投げるところです。<br />
        同じことを思っている投稿には<b>賛同</b>を押してください。多いものから手を付けます。<br />
        状態は運営が動かします。返信がつくとメールでお知らせします。
      </div>
      <div class="title">目安箱</div>
    </div>

    <div v-if="message" class="message error">{{ message }}</div>

    <!-- 詳細 -->
    <template v-if="detail">
      <div class="panel-white">
        <button class="btn mini" @click="closeDetail">← 一覧にもどる</button>
        <div class="post-head">
          <span class="badge" :class="'k-' + detail.post.kind">{{ detail.post.kind_label }}</span>
          <span class="badge" :class="'s-' + detail.post.status">{{
            detail.post.status_label
          }}</span>
          <h3 class="post-title">{{ detail.post.title }}</h3>
        </div>
        <div class="post-meta">
          {{ detail.post.author_name }} ／ {{ fmtDate(detail.post.created_at) }}
        </div>
        <div class="post-body">{{ detail.post.body }}</div>
        <div class="post-actions">
          <button
            class="btn vote"
            :class="{ voted: detail.post.voted }"
            :disabled="busy"
            data-test="vote"
            @click="vote(detail.post.id)"
          >
            {{ detail.post.voted ? '賛同済み' : '賛同する' }} {{ detail.post.votes }}
          </button>
          <button
            v-if="canDelete(detail.post.author_id)"
            class="btn mini danger"
            :disabled="busy"
            @click="removePost(detail.post.id)"
          >
            削除
          </button>
        </div>
        <!-- 運営だけが状態を動かせる -->
        <div v-if="isAdmin" class="staff-row">
          <span class="staff-label">状態を変える:</span>
          <button
            v-for="st in FEEDBACK_STATUSES"
            :key="st.value"
            class="btn mini"
            :class="{ active: detail.post.status === st.value }"
            :disabled="busy"
            @click="setStatus(detail.post.id, st.value)"
          >
            {{ st.label }}
          </button>
        </div>
      </div>

      <div class="panel-white">
        <div class="sec-head">コメント（{{ detail.comments.length }}）</div>
        <div v-if="detail.comments.length === 0" class="muted">まだコメントはありません。</div>
        <div v-for="c in detail.comments" :key="c.id" class="cmt" :class="{ staff: c.is_staff }">
          <div class="cmt-head">
            <span class="cmt-name">{{ c.author_name }}</span>
            <span v-if="c.is_staff" class="staff-tag">運営</span>
            <span class="cmt-date">{{ fmtDate(c.created_at) }}</span>
            <button
              v-if="canDelete(c.author_id)"
              class="btn mini danger"
              :disabled="busy"
              @click="removeComment(c.id)"
            >
              ×
            </button>
          </div>
          <div class="cmt-body">{{ c.body }}</div>
        </div>
        <div class="cmt-form">
          <textarea
            v-model="commentDraft"
            rows="3"
            maxlength="500"
            placeholder="コメント(500文字まで)"
            data-test="comment-input"
          ></textarea>
          <button class="btn primary" :disabled="busy || !commentDraft.trim()" @click="comment">
            コメントする
          </button>
        </div>
      </div>
    </template>

    <!-- 一覧 -->
    <template v-else>
      <div class="panel-white filters">
        <label
          >種別
          <select v-model="filterKind" @change="loadList">
            <option value="">すべて</option>
            <option v-for="k in FEEDBACK_KINDS" :key="k.value" :value="k.value">
              {{ k.label }}
            </option>
          </select>
        </label>
        <label
          >状態
          <select v-model="filterStatus" @change="loadList">
            <option value="">すべて</option>
            <option v-for="st in FEEDBACK_STATUSES" :key="st.value" :value="st.value">
              {{ st.label }}
            </option>
          </select>
        </label>
        <label
          >並び
          <select v-model="sort" @change="loadList">
            <option value="new">新しい順</option>
            <option value="votes">賛同の多い順</option>
          </select>
        </label>
        <button class="btn primary" data-test="new-post" @click="formOpen = !formOpen">
          {{ formOpen ? 'やめる' : '投稿する' }}
        </button>
      </div>

      <div v-if="formOpen" class="panel-white post-form">
        <div class="sec-head">新しい投稿</div>
        <div class="form-row">
          <label
            >種別
            <select v-model="draft.kind">
              <option v-for="k in FEEDBACK_KINDS" :key="k.value" :value="k.value">
                {{ k.label }}
              </option>
            </select>
          </label>
          <label class="grow"
            >タイトル
            <input
              v-model="draft.title"
              maxlength="40"
              placeholder="例: 職業安定所の表がはみ出す"
              data-test="title-input"
            />
          </label>
        </div>
        <textarea
          v-model="draft.body"
          rows="5"
          maxlength="1000"
          placeholder="どの画面で、何をしたら、どうなったかを書いてください(1000文字まで)"
          data-test="body-input"
        ></textarea>
        <div class="actions">
          <button
            class="btn primary"
            :disabled="busy || !draft.title.trim() || !draft.body.trim()"
            data-test="submit"
            @click="submit"
          >
            投稿する
          </button>
        </div>
      </div>

      <div class="panel-white">
        <p v-if="posts.length === 0" class="muted">まだ投稿はありません。</p>
        <table v-else class="fb-table">
          <thead>
            <tr>
              <th>種別</th>
              <th>状態</th>
              <th class="l">タイトル</th>
              <th>賛同</th>
              <th>返信</th>
              <th>投稿者</th>
              <th>日時</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="p in posts"
              :key="p.id"
              class="row"
              :data-test="`post-${p.id}`"
              @click="open(p.id)"
            >
              <td>
                <span class="badge" :class="'k-' + p.kind">{{ p.kind_label }}</span>
              </td>
              <td>
                <span class="badge" :class="'s-' + p.status">{{ p.status_label }}</span>
              </td>
              <td class="l title">{{ p.title }}</td>
              <td :class="{ voted: p.voted }">{{ p.votes }}</td>
              <td>{{ p.comments }}</td>
              <td>{{ p.author_name }}</td>
              <td class="date">{{ fmtDate(p.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <div style="text-align: center; margin-top: 8px">
      <button class="btn" @click="emit('back')">街に戻る</button>
    </div>
  </div>
</template>

<style scoped>
.my-page {
  background-color: #6b5844;
  padding: 6px;
  min-height: 80vh;
}
.btn.back {
  margin-bottom: 6px;
}
.my-header {
  display: flex;
  margin-bottom: 8px;
  border: 1px solid #333;
}
.my-header .lead {
  flex: 1 1 auto;
  background: #fff;
  padding: 8px 12px;
  color: #333;
  line-height: 1.6;
}
.my-header .title {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 150px;
  background: #4a3b2a;
  color: #fff;
  font-size: 18px;
  font-weight: bold;
}
.panel-white {
  background: #fff;
  border: 1px solid #99b;
  padding: 12px;
  margin-bottom: 8px;
}
.sec-head {
  font-weight: bold;
  border-bottom: 1px solid #ddd;
  padding-bottom: 4px;
  margin-bottom: 8px;
}
.muted {
  color: #888;
  font-size: 13px;
}
.filters {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 13px;
}
.filters select {
  margin-left: 4px;
}
/* 種別・状態のバッジ。色で種類が一目で分かるようにする。 */
.badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: bold;
  white-space: nowrap;
  color: #fff;
}
.badge.k-bug {
  background: #c0392b;
}
.badge.k-request {
  background: #2f6fc4;
}
.badge.k-question {
  background: #6b4a9e;
}
.badge.s-open {
  background: #7a8a99;
}
.badge.s-triage {
  background: #d98f28;
}
.badge.s-doing {
  background: #2e9e6b;
}
.badge.s-done {
  background: #2b7a3d;
}
.badge.s-wontfix {
  background: #999;
}
.fb-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.fb-table th {
  background: #efe9df;
  border: 1px solid #d8cfc0;
  padding: 3px 6px;
}
.fb-table td {
  border: 1px solid #eee;
  padding: 3px 6px;
  text-align: center;
}
.fb-table th.l,
.fb-table td.l {
  text-align: left;
}
.fb-table tr.row {
  cursor: pointer;
}
.fb-table tr.row:hover td {
  background: #fbf7f0;
}
.fb-table td.title {
  font-weight: bold;
}
.fb-table td.date {
  color: #777;
  white-space: nowrap;
}
/* 自分が賛同済みの行は数字を強調する。 */
.fb-table td.voted {
  color: #cc6600;
  font-weight: bold;
}
.post-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 8px 0 2px;
  flex-wrap: wrap;
}
.post-title {
  margin: 0;
  font-size: 16px;
}
.post-meta {
  font-size: 12px;
  color: #777;
  margin-bottom: 8px;
}
.post-body {
  white-space: pre-wrap;
  line-height: 1.7;
  background: #fbfbfb;
  border: 1px solid #eee;
  padding: 8px 10px;
}
.post-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
.btn.vote.voted {
  background: #ffe9cc;
  border-color: #d98f28;
  color: #a05a00;
}
.staff-row {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px dashed #ddd;
}
.staff-label {
  font-size: 12px;
  color: #666;
}
.staff-row .btn.active {
  background: #2f6fc4;
  color: #fff;
  border-color: #2f6fc4;
}
.cmt {
  border-bottom: 1px dotted #ddd;
  padding: 6px 2px;
}
/* 運営の返信は色を変えて見つけやすくする。 */
.cmt.staff {
  background: #f2f7ff;
  border-left: 3px solid #2f6fc4;
  padding-left: 8px;
}
.cmt-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #666;
}
.cmt-name {
  font-weight: bold;
  color: #333;
}
.staff-tag {
  background: #2f6fc4;
  color: #fff;
  border-radius: 3px;
  padding: 0 4px;
  font-size: 10px;
}
.cmt-body {
  white-space: pre-wrap;
  line-height: 1.7;
  margin-top: 2px;
}
.cmt-form {
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.cmt-form textarea,
.post-form textarea {
  width: 100%;
  box-sizing: border-box;
  font-family: inherit;
  font-size: 13px;
  padding: 6px;
}
.cmt-form .btn {
  align-self: flex-start;
}
.form-row {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 6px;
  flex-wrap: wrap;
}
.form-row .grow {
  flex: 1 1 260px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.form-row .grow input {
  flex: 1 1 auto;
  padding: 3px 6px;
}
.actions {
  margin-top: 8px;
}
.btn.danger {
  color: #c0392b;
}
</style>
