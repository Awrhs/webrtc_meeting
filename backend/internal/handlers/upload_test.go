package handlers

import "testing"

func TestIsSafeUploadName(t *testing.T) {
	cases := map[string]bool{
		"1700000000_abcdef0123456789.png": true,
		"1700000000_abcdef0123456789.PDF": true,
		"":                                false,
		"..":                              false,
		"../secret.png":                   false,
		"a/b.png":                         false,
		`a\b.png`:                         false,
		"evil.html":                       false, // 不在上传白名单内
		"noext":                           false,
	}
	for name, want := range cases {
		if got := IsSafeUploadName(name); got != want {
			t.Errorf("IsSafeUploadName(%q) = %v, want %v", name, got, want)
		}
	}
}
