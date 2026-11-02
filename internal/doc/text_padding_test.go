package doc

import "testing"

func TestTextPaddingAxesPreserveLegacyAndZero(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"背景色","name":"title","style":{"padding":24}}`)
	run(t, e, `{"type":"updateLayer","id":"title","props":{"paddingX":0}}`)
	l := layer(e, "title")
	if l.S("padding") != 24.0 || l.S("paddingX") != 0.0 || l.S("paddingY") != nil {
		t.Fatalf("horizontal zero must override legacy padding without changing vertical padding: %v", l.Style)
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"paddingY":7.5}}`)
	parsed, err := Parse(e.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	l = parsed.ActiveBoard().Find("title")
	if l.S("paddingX") != 0.0 || l.S("paddingY") != 7.5 || l.S("padding") != 24.0 {
		t.Fatalf("axis values did not survive save/load: %v", l.Style)
	}
	run(t, e, `{"type":"undo"}`)
	if l = layer(e, "title"); l.S("paddingY") != nil || l.S("paddingX") != 0.0 {
		t.Fatalf("undo did not restore the inherited vertical value: %v", l.Style)
	}
	run(t, e, `{"type":"redo"}`)
	if layer(e, "title").S("paddingY") != 7.5 {
		t.Fatal("redo did not restore vertical padding")
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"paddingX":-5}}`)
	if layer(e, "title").S("paddingX") != 0.0 {
		t.Fatal("negative padding must clamp to explicit zero")
	}
}

func TestTextPaddingShorthandAndPresetsResetAxes(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"背景色","name":"title","style":{"padding":12,"paddingX":0,"paddingY":8}}`)
	l := layer(e, "title")
	if l.S("paddingX") != 0.0 || l.S("paddingY") != 8.0 {
		t.Fatalf("explicit axes must win over a shorthand in the same command: %v", l.Style)
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"padding":10}}`)
	l = layer(e, "title")
	if l.S("padding") != 10.0 || l.S("paddingX") != nil || l.S("paddingY") != nil {
		t.Fatalf("uniform shorthand did not reset axis overrides: %v", l.Style)
	}
	run(t, e, `{"type":"updateLayer","id":"title","props":{"paddingX":0,"paddingY":6}}`)
	run(t, e, `{"type":"applyTextPreset","id":"title","preset":"yellow-box"}`)
	l = layer(e, "title")
	if l.S("padding") != 24.0 || l.S("paddingX") != nil || l.S("paddingY") != nil {
		t.Fatalf("preset padding was overridden by old axis values: %v", l.Style)
	}
}

func TestScaleTextPaddingAxes(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"背景色","name":"title","style":{"padding":12,"paddingX":0,"paddingY":8}}`)
	l := layer(e, "title")
	bakeScale(l, 2, 2)
	if l.S("padding") != 24.0 || l.S("paddingX") != 0.0 || l.S("paddingY") != 16.0 {
		t.Fatalf("scaling must retain independent padding and zero overrides: %v", l.Style)
	}
}
