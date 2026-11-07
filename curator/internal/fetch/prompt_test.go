package fetch_test

import (
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
)

func TestPromptDealsSectionOnlyForDealTopics(t *testing.T) {
	since := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	news := fetch.Group{Topics: []fetch.Topic{{Slug: "languages/go", Name: "Go"}}, Since: since, PerTopic: 5}
	deals := fetch.Group{Topics: []fetch.Topic{{Slug: "deals/laptops", Name: "Laptop Deals"}}, Since: since, PerTopic: 5}

	for _, style := range []config.PromptStyle{config.PromptFrontier, config.PromptCompact} {
		if p := fetch.Prompt(style, news, nil); strings.Contains(p, "## Deals") {
			t.Errorf("%s prompt for a news topic has the deals section", style)
		}
		p := fetch.Prompt(style, deals, nil)
		if !strings.Contains(p, "## Deals") || strings.Contains(p, "%%") || !strings.Contains(p, "10%") {
			t.Errorf("%s prompt for a deal topic lacks a clean deals section:\n%s", style, p)
		}
	}
}

func TestPromptKnownStoriesListTitlesOnly(t *testing.T) {
	g := fetch.Group{Topics: []fetch.Topic{{Slug: "languages/go", Name: "Go"}}, Since: time.Now(), PerTopic: 5}
	p := fetch.Prompt(config.PromptFrontier, g, []fetch.StoryRef{{Title: "Go 1.26 released"}})
	if !strings.Contains(p, "## Already covered") || !strings.Contains(p, "- Go 1.26 released\n") {
		t.Errorf("prompt lacks the known story:\n%s", p)
	}
}
