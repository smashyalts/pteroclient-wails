package pteroapi

import "testing"

func TestClassifySignedURL(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		parallel bool
		reusable bool
	}{
		{
			// The only way to tell on a stock panel with a client key: the
			// client API's backup record has no disk field.
			name:     "s3 sigv4",
			url:      "https://bucket.s3.amazonaws.com/uuid/backup.tar.gz?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc",
			parallel: true, reusable: true,
		},
		{
			name:     "r2 presigned",
			url:      "https://acct.r2.cloudflarestorage.com/b/u.tar.gz?x-amz-signature=deadbeef&x-amz-expires=300",
			parallel: true, reusable: true,
		},
		{
			name:     "sigv2",
			url:      "https://bucket.s3.amazonaws.com/u.tar.gz?AWSAccessKeyId=AKIA&Signature=xyz",
			parallel: true, reusable: true,
		},
		{
			// A wings token is spent on first use, so it must never be shared
			// between connections.
			name:     "wings node",
			url:      "https://node.example.com:8080/download/backup?token=eyJhbGciOi",
			parallel: false, reusable: false,
		},
		{
			// Unknown shapes get the cautious answer; the range probe still
			// discovers the truth at runtime.
			name:     "something else",
			url:      "https://cdn.example.com/archives/u.tar.gz",
			parallel: false, reusable: false,
		},
	}

	for _, tc := range cases {
		got := ClassifySignedURL(tc.url)
		if got.Parallel != tc.parallel {
			t.Errorf("%s: Parallel = %v, want %v", tc.name, got.Parallel, tc.parallel)
		}
		if got.ReusableURL != tc.reusable {
			t.Errorf("%s: ReusableURL = %v, want %v", tc.name, got.ReusableURL, tc.reusable)
		}
		if got.Note == "" {
			t.Errorf("%s: no note to show the user", tc.name)
		}
	}
}

func TestSuggestedFilename(t *testing.T) {
	cases := map[string]string{
		"nightly":        "nightly.tar.gz",
		"nightly.tar.gz": "nightly.tar.gz",
		"bad/name:here":  "bad-name-here.tar.gz",
		"":               "backup.tar.gz",
	}
	for name, want := range cases {
		record := &BackupRecord{Backup: Backup{Name: name, UUID: ""}}
		if got := SuggestedFilename(record); got != want {
			t.Errorf("SuggestedFilename(%q) = %q, want %q", name, got, want)
		}
	}
	// A nameless backup falls back to its UUID rather than a shared default
	// that would collide with every other nameless one.
	record := &BackupRecord{Backup: Backup{UUID: "1a7ce997-dead-beef"}}
	if got := SuggestedFilename(record); got != "1a7ce997-dead-beef.tar.gz" {
		t.Errorf("got %q", got)
	}
}
