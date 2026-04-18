package provider

import "testing"

func TestQueryValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query Query
		want  bool
	}{
		"valid": {
			query: Query{Count: 1, MinWidth: 0, MinHeight: 0, SizeBucket: "medium"},
			want:  true,
		},
		"count must be positive": {
			query: Query{Count: 0},
		},
		"min width must be non-negative": {
			query: Query{Count: 1, MinWidth: -1},
		},
		"min height must be non-negative": {
			query: Query{Count: 1, MinHeight: -1},
		},
		"size must be known": {
			query: Query{Count: 1, SizeBucket: "huge"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.query.Validate()
			if tc.want && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}

			if !tc.want && err == nil {
				t.Fatal("Validate() error = nil; want invalid query")
			}
		})
	}
}

func TestFitsSizeBucket(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bucket string
		width  int
		want   bool
	}{
		"empty bucket":                      {bucket: "", width: 1200, want: true},
		"unknown width":                     {bucket: "small", width: 0, want: true},
		"small matches":                     {bucket: "small", width: 640, want: true},
		"medium matches":                    {bucket: "medium", width: 1200, want: true},
		"large matches":                     {bucket: "large", width: 2000, want: true},
		"unknown width passes valid bucket": {bucket: "large", width: 0, want: true},
		"invalid fails shut":                {bucket: "huge", width: 2000, want: false},
		"invalid unknown width fails shut":  {bucket: "huge", width: 0, want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := fitsSizeBucket(tc.bucket, tc.width); got != tc.want {
				t.Fatalf("fitsSizeBucket(%q, %d) = %v; want %v", tc.bucket, tc.width, got, tc.want)
			}
		})
	}
}
