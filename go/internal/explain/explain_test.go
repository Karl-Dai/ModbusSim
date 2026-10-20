package explain

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestGoldens replays testdata/cases.txt against output captured from the Rust
// implementation, so the ported renderer has to agree byte for byte.
func TestGoldens(t *testing.T) {
	contents, err := os.ReadFile("testdata/cases.txt")
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	for i, line := range lines {
		i := i
		line := line
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			// Line format: data|transport|direction|lang. The frames carry no
			// '|', so a plain split is enough.
			parts := strings.Split(line, "|")
			if len(parts) != 4 {
				t.Fatalf("malformed case %q", line)
			}
			got, err := Explain(parts[0], parts[1], parts[2], LangFromCode(parts[3]))
			text := got
			if err != nil {
				text = "!!ERROR!!\n" + err.Error()
			}
			want, err := os.ReadFile(fmt.Sprintf("testdata/golden/%03d.txt", i))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if text != string(want) {
				t.Errorf("output differs\n--- got ---\n%s\n--- want ---\n%s", text, want)
			}
		})
	}
}

func TestLangFromCode(t *testing.T) {
	for _, code := range []string{"zh", "zh-CN", "zh-TW"} {
		if got := LangFromCode(code); got != Zh {
			t.Errorf("LangFromCode(%q) = %v, want Zh", code, got)
		}
	}
	for _, code := range []string{"en", "ja", ""} {
		if got := LangFromCode(code); got != En {
			t.Errorf("LangFromCode(%q) = %v, want En", code, got)
		}
	}
}

func TestRejectsBadDirection(t *testing.T) {
	for _, lang := range []Lang{Zh, En} {
		_, err := Explain("00 01 00 00 00 06 01 03 00 00 00 01", "tcp", "both", lang)
		if err == nil {
			t.Fatalf("lang %v: expected an error", lang)
		}
		want := lang.pick("方向参数无效：both（可选 auto / request / response）",
			"Invalid direction: both (use auto, request or response)")
		if err.Error() != want {
			t.Errorf("got %q, want %q", err, want)
		}
	}
}

// The byte table pads by display width, so a wide label must line up with the
// ASCII rows around it.
func TestDisplayWidthAlignment(t *testing.T) {
	if w := displayWidth("CRC 校验"); w != 8 {
		t.Errorf("displayWidth(\"CRC 校验\") = %d, want 8", w)
	}
	if got := pad("ab", 6); got != "ab    " {
		t.Errorf("pad = %q", got)
	}
	if got := pad("长长长的", 6); got != "长长长的" {
		t.Errorf("pad must not truncate over-wide text, got %q", got)
	}
}
