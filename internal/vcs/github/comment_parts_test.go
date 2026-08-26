package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/google/go-github/v66/github"
)

func TestPartOrdinal(t *testing.T) {
	const prefix = "<!-- reeve:pr-comment:v1"
	cases := []struct {
		name string
		body string
		want int
		ok   bool
	}{
		{"bare board marker is part 1", "<!-- reeve:pr-comment:v1 -->\n## board", 1, true},
		{"part 2", "<!-- reeve:pr-comment:v1:part2 -->\nsections", 2, true},
		{"part 10", "<!-- reeve:pr-comment:v1:part10 -->\nsections", 10, true},
		{"another marker entirely", "<!-- reeve:help -->\nhelp", 0, false},
		// A section board's marker carries the SHA, so the bare prefix must not
		// claim it as part 1 of the replace-style board.
		{"section marker is not the replace board", "<!-- reeve:pr-comment:v1:abc1234 -->", 0, false},
		{"timeline marker", "<!-- reeve:timeline:v1:abc1234 -->", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := partOrdinal(c.body, prefix)
			if ok != c.ok || got != c.want {
				t.Fatalf("partOrdinal = (%d, %v), want (%d, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}

// A section board's own parts must resolve against its own prefix.
func TestPartOrdinalSectionBoard(t *testing.T) {
	const prefix = "<!-- reeve:pr-comment:v1:abc1234"
	if got, ok := partOrdinal("<!-- reeve:pr-comment:v1:abc1234 -->", prefix); !ok || got != 1 {
		t.Fatalf("section part 1: got (%d, %v)", got, ok)
	}
	if got, ok := partOrdinal("<!-- reeve:pr-comment:v1:abc1234:part3 -->", prefix); !ok || got != 3 {
		t.Fatalf("section part 3: got (%d, %v)", got, ok)
	}
}

func TestDeleteCommentsOnlyRemovesAuthenticatedAuthorsParts(t *testing.T) {
	deleted := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"reeve-bot"}`))
	})
	mux.HandleFunc("/repos/acme/repo/issues/12/comments", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":2,"body":"<!-- reeve:pr-comment:v1:part2 -->","user":{"login":"reeve-bot"}},
			{"id":3,"body":"<!-- reeve:pr-comment:v1:part3 -->","user":{"login":"attacker"}}
		]`))
	})
	mux.HandleFunc("/repos/acme/repo/issues/comments/2", func(w http.ResponseWriter, _ *http.Request) {
		deleted["2"] = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/repos/acme/repo/issues/comments/3", func(w http.ResponseWriter, _ *http.Request) {
		deleted["3"] = true
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	api := gh.NewClient(server.Client())
	base, _ := api.BaseURL.Parse(server.URL + "/")
	api.BaseURL = base
	client := &Client{gh: api, owner: "acme", repo: "repo"}

	n, err := client.DeleteCommentsByMarkerPrefix(context.Background(), 12,
		"<!-- reeve:pr-comment:v1", 1)
	if err != nil {
		t.Fatalf("DeleteCommentsByMarkerPrefix: %v", err)
	}
	if n != 1 || !deleted["2"] || deleted["3"] {
		t.Fatalf("deleted=%v count=%d; must preserve the copied marker", deleted, n)
	}
}
