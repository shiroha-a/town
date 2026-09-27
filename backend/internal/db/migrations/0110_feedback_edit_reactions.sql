-- +goose Up
-- 目安箱の編集とリアクション。仕様: .tmp/design_meyasu_edit_react.md

-- 本人が書き直した時刻(NULL=未編集)。画面に「編集済み」を出すためだけに持ち、
-- 履歴は残さない。updated_at は状態変更でも動くので、編集の印には使えない。
ALTER TABLE feedback_posts ADD COLUMN edited_at TIMESTAMPTZ;
ALTER TABLE feedback_comments ADD COLUMN edited_at TIMESTAMPTZ;

-- リアクション。reaction は許可リストのUnicode絵文字か、使用許可済みの
-- カスタム絵文字の :name@host:。1人が同じ対象に違う種類を複数付けられる(GitHub式)。
-- 投稿とコメントで表を分け、削除・退会の後始末をFKのCASCADEに任せる。
CREATE TABLE feedback_post_reactions (
    post_id    BIGINT NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    reaction   TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, player_id, reaction)
);

CREATE TABLE feedback_comment_reactions (
    comment_id BIGINT NOT NULL REFERENCES feedback_comments(id) ON DELETE CASCADE,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    reaction   TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, player_id, reaction)
);

-- +goose Down
DROP TABLE feedback_comment_reactions;
DROP TABLE feedback_post_reactions;
ALTER TABLE feedback_comments DROP COLUMN edited_at;
ALTER TABLE feedback_posts DROP COLUMN edited_at;
