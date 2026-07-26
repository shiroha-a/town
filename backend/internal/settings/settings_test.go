package settings

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	base := Defaults()
	cases := []struct {
		name    string
		mutate  func(*Game)
		wantErr bool
	}{
		{"defaults are valid", func(*Game) {}, false},
		{"unknown timezone", func(g *Game) { g.Timezone = "Foo/Bar" }, true},
		{"empty timezone is UTC", func(g *Game) { g.Timezone = "" }, false},
		{"boundary 0 is valid", func(g *Game) { g.DayBoundaryHour = 0 }, false},
		{"boundary 23 is valid", func(g *Game) { g.DayBoundaryHour = 23 }, false},
		{"boundary 24 is out of range", func(g *Game) { g.DayBoundaryHour = 24 }, true},
		{"negative boundary", func(g *Game) { g.DayBoundaryHour = -1 }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := base
			c.mutate(&g)
			err := g.Validate()
			if c.wantErr && !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
			if !c.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestDefaultsMatchLegacy(t *testing.T) {
	g := Defaults()
	// レガシー準拠の代表値。既定を変えるときはここも意識的に直す。
	if g.InitialMoney != 500000 {
		t.Errorf("InitialMoney = %d, want 500000", g.InitialMoney)
	}
	if g.DayBoundaryHour != 5 {
		t.Errorf("DayBoundaryHour = %d, want 5", g.DayBoundaryHour)
	}
	if g.ItemKindLimit != 25 {
		t.Errorf("ItemKindLimit = %d, want 25", g.ItemKindLimit)
	}
	if g.DebugNoCooldown {
		t.Error("DebugNoCooldown は既定でfalseであるべき(本番既定)")
	}
}
