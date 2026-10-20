package nativedialog

import "testing"

func TestOpenFileExtensions(t *testing.T) {
	got := openFileExtensions([]Filter{
		{Name: "PEM Certificate", Extensions: []string{"pem", "crt", ".pem", " key "}},
		{Name: "Keys", Extensions: []string{"", "key"}},
	})
	if want := "pem;crt;key"; got != want {
		t.Fatalf("openFileExtensions() = %q, want %q", got, want)
	}
}

func TestOpenFileExtensionsEmpty(t *testing.T) {
	if got := openFileExtensions(nil); got != "" {
		t.Fatalf("openFileExtensions(nil) = %q, want empty", got)
	}
}

func TestOpenFileMessage(t *testing.T) {
	if got := openFileMessage([]Filter{{Name: "  "}, {Name: "PEM Certificate"}}); got != "PEM Certificate" {
		t.Fatalf("openFileMessage() = %q", got)
	}
	if got := openFileMessage(nil); got != "" {
		t.Fatalf("openFileMessage(nil) = %q, want empty", got)
	}
}
