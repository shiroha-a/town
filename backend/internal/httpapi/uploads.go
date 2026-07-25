package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/shiroha-a/town/internal/content"
)

// アップロード画像の制約。背景タイル等の小さな画像を想定。
const maxUploadBytes = 1 << 20 // 1MB

var (
	uploadNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	allowedMime  = map[string]bool{"image/png": true, "image/gif": true, "image/jpeg": true, "image/webp": true}
)

type uploadAssetReq struct {
	Name string `json:"name"` // URLスラッグ([a-zA-Z0-9_-]、40字以内)
	Mime string `json:"mime"`
	Data string `json:"data"` // base64(データURLのdata:部分ではなく本体)
}

// adminUploadAsset stores an uploaded image (admin). Overwrites同名。
func (s *Server) adminUploadAsset(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req uploadAssetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !uploadNameRe.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, "画像名は英数字・ハイフン・アンダースコア(40字以内)にしてください。")
		return
	}
	if !allowedMime[strings.ToLower(req.Mime)] {
		writeError(w, http.StatusBadRequest, "対応していない画像形式です(png/gif/jpeg/webp)。")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像データが不正です。")
		return
	}
	if len(data) == 0 || len(data) > maxUploadBytes {
		writeError(w, http.StatusBadRequest, "画像サイズが不正です(1MB以内)。")
		return
	}
	if err := s.content.SaveImage(r.Context(), req.Name, strings.ToLower(req.Mime), data); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name})
}

// adminListAssets returns uploaded image names (for the palette).
func (s *Server) adminListAssets(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	names, err := s.content.ListImageNames(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, names)
}

// adminDeleteAsset deletes an uploaded image (admin). 配置中は拒否する。
func (s *Server) adminDeleteAsset(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	name := r.PathValue("name")
	used, err := s.content.ImageInUse(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if used {
		writeError(w, http.StatusUnprocessableEntity, "この画像は背景に配置されています。先に配置を外してください。")
		return
	}
	if err := s.content.DeleteImage(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// serveAsset serves an uploaded image by name (public; players load it).
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	mime, data, err := s.content.GetImage(r.Context(), name)
	if err != nil {
		if errors.Is(err, content.ErrImageNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
