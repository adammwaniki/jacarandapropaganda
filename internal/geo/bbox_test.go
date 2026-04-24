package geo

import "testing"

func TestParseBbox_Nairobi(t *testing.T) {
	t.Parallel()
	b, err := ParseBbox("36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Bbox{MinLon: 36.6, MinLat: -1.4, MaxLon: 37.0, MaxLat: -1.1}
	if b != want {
		t.Fatalf("got %+v, want %+v", b, want)
	}
}

func TestParseBbox_Rejects(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":            "",
		"too few":          "1,2,3",
		"too many":         "1,2,3,4,5",
		"not a number":     "a,b,c,d",
		"inverted lon":     "37,-1.4,36,-1.1",
		"inverted lat":     "36.6,-1.1,37,-1.4",
		"lon out of range": "200,-1.4,210,-1.1",
		"lat out of range": "36.6,-100,37,100",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseBbox(input); err == nil {
				t.Fatalf("expected error for %q, got nil", input)
			}
		})
	}
}
