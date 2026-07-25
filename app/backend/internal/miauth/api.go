package miauth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// UserDetailed is the subset of Misskey's UserDetailed we display.
type UserDetailed struct {
	ID             string `json:"id"`
	Username       string `json:"username"`
	Host           string `json:"host"` // null for a user local to the queried instance
	Name           string `json:"name"`
	AvatarURL      string `json:"avatarUrl"`
	BannerURL      string `json:"bannerUrl"`
	Description    string `json:"description"`
	FollowersCount int    `json:"followersCount"`
	FollowingCount int    `json:"followingCount"`
	NotesCount     int    `json:"notesCount"`
	IsBot          bool   `json:"isBot"`
	IsCat          bool   `json:"isCat"`
	IsLocked       bool   `json:"isLocked"`
	// Relations, only present when the call carried a token.
	IsFollowing                    bool `json:"isFollowing"`
	HasPendingFollowRequestFromYou bool `json:"hasPendingFollowRequestFromYou"`
}

// ShowUser fetches a profile from the instance that hosts it. users/show is
// requireCredential:false, so no token is needed; pass one only to learn the
// viewer's relation to that user (isFollowing).
func (c *Client) ShowUser(ctx context.Context, host, token, userID string) (*UserDetailed, error) {
	var u UserDetailed
	if err := c.postJSON(ctx, host, "/api/users/show", token, map[string]any{"userId": userID}, &u); err != nil {
		return nil, err
	}
	if u.ID == "" {
		return nil, fmt.Errorf("ユーザーが見つかりません。")
	}
	return &u, nil
}

// ShowUserByAcct looks an account up on the *viewer's* instance by
// username@host, returning the id that instance knows it by along with the
// viewer's relation to it (isFollowing). A follow must be issued on the
// viewer's instance with that instance's ids, so this is the resolution step.
//
// users/show resolves unknown remote accounts itself (RemoteUserResolveService)
// and carries no rate limit, unlike ap/show which costs one of 30 calls per
// hour and needs read:account. Passing the token is required: instances with
// ugcVisibilityForVisitor=local refuse the lookup for anonymous callers.
//
// remoteHost is empty when the account is local to the viewer's instance.
func (c *Client) ShowUserByAcct(ctx context.Context, viewerHost, token, username, remoteHost string) (*UserDetailed, error) {
	body := map[string]any{"username": username, "host": nil}
	if remoteHost != "" && remoteHost != viewerHost {
		body["host"] = remoteHost
	}
	var u UserDetailed
	if err := c.postJSON(ctx, viewerHost, "/api/users/show", token, body, &u); err != nil {
		return nil, err
	}
	if u.ID == "" {
		return nil, fmt.Errorf("相手のアカウントを解決できませんでした。")
	}
	return &u, nil
}

// Follow follows userID on the viewer's own instance. Requires write:following.
func (c *Client) Follow(ctx context.Context, viewerHost, token, userID string) error {
	return c.postJSON(ctx, viewerHost, "/api/following/create", token, map[string]any{"userId": userID}, nil)
}

// Unfollow undoes Follow.
func (c *Client) Unfollow(ctx context.Context, viewerHost, token, userID string) error {
	return c.postJSON(ctx, viewerHost, "/api/following/delete", token, map[string]any{"userId": userID}, nil)
}

// ProfileURL is the human-facing page of an account.
func ProfileURL(host, username string) string {
	return "https://" + host + "/@" + username
}

// RemoteFollowURL is the fallback: the standard OStatus subscribe entry point on
// the viewer's own instance. It needs no token, no scope and no rate budget, but
// the viewer has to confirm on their instance.
func RemoteFollowURL(viewerHost, acct string) string {
	return "https://" + viewerHost + "/authorize-follow?acct=" + url.QueryEscape(strings.TrimPrefix(acct, "@"))
}

// Acct renders @user@host. host is empty for a user local to their own instance,
// in which case the account's own instance is used.
func Acct(username, host, ownHost string) string {
	if host == "" {
		host = ownHost
	}
	return "@" + username + "@" + host
}
