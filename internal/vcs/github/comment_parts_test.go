package github

import "testing"

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
