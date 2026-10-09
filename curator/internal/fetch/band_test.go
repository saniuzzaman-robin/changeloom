package fetch_test

import (
	"slices"
	"testing"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
)

func TestParseBand(t *testing.T) {
	tests := []struct {
		in      string
		want    fetch.Band
		wantErr bool
	}{
		{in: "", want: fetch.Band{}},
		{in: "1-100", want: fetch.Band{From: 1, To: 100}},
		{in: " 101 - 400 ", want: fetch.Band{From: 101, To: 400}},
		{in: "401-", want: fetch.Band{From: 401}},
		{in: "5-5", want: fetch.Band{From: 5, To: 5}},
		{in: "100", wantErr: true},
		{in: "0-10", wantErr: true},
		{in: "-10", wantErr: true},
		{in: "10-5", wantErr: true},
		{in: "a-b", wantErr: true},
	}
	for _, tc := range tests {
		got, err := fetch.ParseBand(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseBand(%q) = %+v, %v; want %+v, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func rankTopics() []fetch.Topic {
	return []fetch.Topic{
		{Slug: "root", HasChildren: true, Engaged7d: 3},
		{Slug: "root/low", ParentSlug: "root", Priority: 3},
		{Slug: "root/top", ParentSlug: "root", Priority: 1},
		{Slug: "root/busy", ParentSlug: "root", Priority: 2, Engaged7d: 2},
		{Slug: "root/quiet", ParentSlug: "root", Priority: 2},
		{Slug: "solo", Priority: 2, Engaged7d: 5, Views7d: 1},
		{Slug: "unset", Priority: 0},
		{Slug: "other/a", ParentSlug: "other", Priority: 2, Views7d: 9},
		{Slug: "other", HasChildren: true},
	}
}

func TestRankOrdersByPriorityThenDemand(t *testing.T) {
	got := fetch.Rank(rankTopics())
	// Priority first; within priority 2 the topic's own plus its parent's users (solo 5, busy 2+3, quiet 0+3),
	// then viewers (other/a), then slug. The unset priority counts as the default (3), so it joins "low".
	want := []string{"root/top", "solo", "root/busy", "root/quiet", "other/a", "root/low", "unset"}
	if !slices.Equal(got, want) {
		t.Fatalf("Rank = %v, want %v", got, want)
	}
}

func TestBandSlugs(t *testing.T) {
	topics := rankTopics()
	all := fetch.Rank(topics)
	tests := []struct {
		band fetch.Band
		want []string
	}{
		{fetch.Band{}, all},
		{fetch.Band{From: 1, To: 2}, all[:2]},
		{fetch.Band{From: 3, To: 4}, all[2:4]},
		{fetch.Band{From: 6}, all[5:]},
		{fetch.Band{From: 1, To: 100}, all},
		{fetch.Band{From: 8}, nil},
	}
	for _, tc := range tests {
		if got := tc.band.Slugs(topics); !slices.Equal(got, tc.want) {
			t.Errorf("band %q = %v, want %v", tc.band.String(), got, tc.want)
		}
	}
}
