package emoji

import (
	"testing"

	"github.com/shiroha-a/town/internal/miauth"
)

func TestRefs(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Ref
	}{
		{"plain text has none", "こんにちは", nil},
		{
			"host is required",
			"こんにちは :kusa: いい天気",
			nil,
		},
		{
			"picks up name@host",
			"こんにちは :kusa@misskey.example: いい天気",
			[]Ref{{Name: "kusa", Host: "misskey.example"}},
		},
		{
			"deduplicates and lowercases the host",
			":a@Misskey.Example: :a@misskey.example: :b@other.example:",
			[]Ref{{Name: "a", Host: "misskey.example"}, {Name: "b", Host: "other.example"}},
		},
		{
			"underscores and digits are valid shortcodes",
			":blob_cat_2@mk.example:",
			[]Ref{{Name: "blob_cat_2", Host: "mk.example"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Refs(c.text)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("[%d] got %v, want %v", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestCountRefsCountsOccurrences(t *testing.T) {
	// Refs は重複を畳むが、1投稿あたりの上限は「出現数」で数える。
	text := ":a@x.example: :a@x.example: :b@x.example:"
	if got := CountRefs(text); got != 3 {
		t.Errorf("CountRefs = %d, want 3", got)
	}
	if got := len(Refs(text)); got != 2 {
		t.Errorf("len(Refs) = %d, want 2", got)
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		name  string
		emoji miauth.EmojiDetailed
		want  string
	}{
		{"usable", miauth.EmojiDetailed{License: "CC BY 4.0"}, ""},
		{"no license", miauth.EmojiDetailed{License: ""}, ReasonNoLicense},
		{"blank license", miauth.EmojiDetailed{License: "   "}, ReasonNoLicense},
		{"sensitive", miauth.EmojiDetailed{License: "CC0", IsSensitive: true}, ReasonSensitive},
		{"local only", miauth.EmojiDetailed{License: "CC0", LocalOnly: true}, ReasonLocalOnly},
		{
			"local only wins over a missing license",
			miauth.EmojiDetailed{LocalOnly: true},
			ReasonLocalOnly,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := verdict(&c.emoji); got != c.want {
				t.Errorf("verdict = %q, want %q", got, c.want)
			}
		})
	}
}

func TestShortcode(t *testing.T) {
	e := Emoji{Host: "misskey.example", Name: "kusa"}
	if got := e.Shortcode(); got != ":kusa@misskey.example:" {
		t.Errorf("Shortcode = %q", got)
	}
}
