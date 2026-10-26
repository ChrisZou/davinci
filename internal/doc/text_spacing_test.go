package doc

import "testing"

func TestPixelLetterSpacingCommandsAndHistory(t *testing.T) {
	e, f := engine(t)
	run(t, e, `{"type":"addText","text":"告别抽卡","name":"title","style":{"fontSize":250,"letterSpacing":50}}`)
	if got := layer(e, "title").S("letterSpacing"); got != 50.0 {
		t.Fatalf("addText pixel spacing = %v", got)
	}
	n := f.measured
	run(t, e, `{"type":"updateLayer","id":"title","props":{"letterSpacing":12.5}}`)
	if f.measured != n+1 {
		t.Fatal("pixel spacing did not invalidate text measurements")
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"fontSize":100}}`)
	if layer(e, "title").S("letterSpacing") != 12.5 {
		t.Fatal("changing font size changed pixel spacing")
	}
	parsed, err := Parse(e.Snapshot())
	if err != nil || parsed.ActiveBoard().Find("title").S("letterSpacing") != 12.5 {
		t.Fatalf("pixel spacing did not survive save/load: %v", err)
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"letterSpacing":-5}}`)
	run(t, e, `{"type":"undo"}`)
	if layer(e, "title").S("letterSpacing") != 12.5 {
		t.Fatal("undo lost pixel spacing")
	}
	run(t, e, `{"type":"redo"}`)
	if layer(e, "title").S("letterSpacing") != -5.0 {
		t.Fatal("redo lost negative spacing")
	}
}

func TestLegacySpacingCompatibility(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"告别抽卡","name":"title","style":{"fontSize":250,"charSpacing":50}}`)
	run(t, e, `{"type":"updateLayer","id":"title","props":{"fontSize":100}}`)
	l := layer(e, "title")
	if l.S("letterSpacing") != 12.5 || l.S("charSpacing") != nil {
		t.Fatalf("font-size edit did not preserve displayed pixel spacing: %v", l.Style)
	}
	run(t, e, `{"type":"undo"}`)
	if l = layer(e, "title"); l.S("charSpacing") != 50.0 || l.S("letterSpacing") != nil {
		t.Fatal("undo did not restore legacy style")
	}
	// Clearing spacing must not reveal an old charSpacing value underneath it.
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"letterSpacing":0}}`)
	if l = layer(e, "title"); l.S("charSpacing") != nil || l.S("letterSpacing") != nil {
		t.Fatalf("clearing spacing left old tracking: %v", l.Style)
	}
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"letterSpacing":50}}`)
	run(t, e, `{"type":"setTextStyle","id":"title","style":{"charSpacing":20}}`)
	if l = layer(e, "title"); l.S("letterSpacing") != nil || l.S("charSpacing") != 20.0 {
		t.Fatal("legacy API edit was overridden by pixel spacing")
	}
	run(t, e, `{"type":"updateLayer","id":"title","props":{"charSpacing":50,"letterSpacing":0}}`)
	if l = layer(e, "title"); l.S("charSpacing") != nil || l.S("letterSpacing") != nil {
		t.Fatal("explicit pixel spacing must take precedence over legacy tracking")
	}
}

func TestScalingTextScalesPixelSpacing(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"告别抽卡","name":"title","style":{"fontSize":100,"letterSpacing":12.5}}`)
	l := layer(e, "title")
	bakeScale(l, 2, 2)
	if l.S("fontSize") != 200.0 || l.S("letterSpacing") != 25.0 {
		t.Fatalf("scaling the whole text must scale both glyphs and spacing: %v", l.Style)
	}
}
