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
