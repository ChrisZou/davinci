package server

import "testing"

func TestAutoHidden(t *testing.T) {
	for _, f := range []string{".SF Adaptive Numeric", ".CJK Symbols Fallback SC", "Al Bayan", "Noto Sans Adlam", "Tamil Sangam MN", "BM Jua", "Apple Color Emoji", "STIXGeneral"} {
		if !autoHidden(f) {
			t.Errorf("%q should be hidden", f)
		}
	}
	for _, f := range []string{"PingFang SC", "HarmonyOS Sans SC", "Noto Sans CJK SC", "Noto Serif SC", "Futura", "Arial Black", "Songti SC", "YouSheBiaoTiHei"} {
		if autoHidden(f) {
			t.Errorf("%q should be shown", f)
		}
	}
}

func TestAutoHiddenNotoCJKWeightSuffix(t *testing.T) {
	visible := []string{
		"Noto Sans SC", "Noto Serif TC", "Noto Sans SC Black", "Noto Sans SC Medium",
		"Noto Sans JP Bold", "Noto Sans KR Light", "Noto Serif HK ExtraLight",
		"Noto Sans TC SemiBold", "Noto Sans SC Thin", "Noto Sans SC Heavy",
		"Noto Sans SC Bold Italic", "Noto Sans CJK SC",
	}
	for _, f := range visible {
		if autoHidden(f) {
			t.Errorf("autoHidden(%q) = true, want visible", f)
		}
	}
	hidden := []string{
		"Noto Sans Arabic", "Noto Sans Thai Bold", "Noto Serif Hebrew Medium",
		"Noto Sans Devanagari", "Noto Sans SC Foo", "Noto Sans SCBlack",
	}
	for _, f := range hidden {
		if !autoHidden(f) {
			t.Errorf("autoHidden(%q) = false, want hidden", f)
		}
	}
}
