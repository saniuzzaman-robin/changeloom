package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

func (e *env) makeDeal(id int64, countries ...string) {
	e.t.Helper()
	if countries == nil {
		countries = []string{}
	}
	if _, err := e.pool.Exec(e.t.Context(), `UPDATE stories SET kind = 'deal', countries = $2 WHERE id = $1`, id, countries); err != nil {
		e.t.Fatalf("make deal: %v", err)
	}
}

func TestDealsFilteredByCountry(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	news := e.insertStory("Go 1.30 released", now.Add(-5*time.Hour), "languages/go")
	us := e.insertStory("Laptop deal US", now.Add(-4*time.Hour), "languages/go")
	bd := e.insertStory("Laptop deal BD", now.Add(-3*time.Hour), "languages/go")
	global := e.insertStory("Laptop deal global", now.Add(-2*time.Hour), "languages/go")
	e.makeDeal(us, "US")
	e.makeDeal(bd, "BD", "IN")
	e.makeDeal(global)

	visible := func(token string) []int64 {
		got := ids(e.timeline(token, 50, "").Items)
		slices.Sort(got)
		return got
	}
	sorted := func(v ...int64) []int64 { slices.Sort(v); return v }

	// No country set: only non-deals and global deals.
	if got, want := visible(aliceToken), sorted(news, global); !slices.Equal(got, want) {
		t.Fatalf("no country = %v, want %v", got, want)
	}

	var me struct {
		Country *string `json:"country"`
	}
	if code := e.do(http.MethodPut, "/v1/me/country", aliceToken, map[string]any{"country": "BD"}, &me); code != http.StatusOK {
		t.Fatalf("put country: status %d", code)
	}
	if me.Country == nil || *me.Country != "BD" {
		t.Fatalf("me.country = %v, want BD", me.Country)
	}
	if got, want := visible(aliceToken), sorted(news, bd, global); !slices.Equal(got, want) {
		t.Fatalf("BD = %v, want %v", got, want)
	}
	// Another user is unaffected.
	if got, want := visible(bobToken), sorted(news, global); !slices.Equal(got, want) {
		t.Fatalf("bob = %v, want %v", got, want)
	}

	var search timelinePage
	e.do(http.MethodGet, "/v1/search?q=laptop", aliceToken, nil, &search)
	got := ids(search.Items)
	slices.Sort(got)
	if want := sorted(bd, global); !slices.Equal(got, want) {
		t.Fatalf("search = %v, want %v", got, want)
	}

	// Clearing the country hides country-specific deals again.
	e.do(http.MethodPut, "/v1/me/country", aliceToken, map[string]any{"country": nil}, nil)
	if got, want := visible(aliceToken), sorted(news, global); !slices.Equal(got, want) {
		t.Fatalf("cleared = %v, want %v", got, want)
	}
}

func TestPutMyCountryRejectsBadCode(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"bd", "BGD", "B", ""} {
		if code := e.do(http.MethodPut, "/v1/me/country", aliceToken, map[string]any{"country": c}, nil); code != http.StatusBadRequest {
			t.Errorf("country %q: status %d, want 400", c, code)
		}
	}
}
