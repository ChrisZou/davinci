package doc

// CanvasPreset is a stock canvas size for the platforms 创哥 posts to.
type CanvasPreset struct {
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// CanvasPresets are offered by `davinci new`, the home page and setCanvasSize.
var CanvasPresets = []CanvasPreset{
	{Key: "xhs-3-4", Name: "小红书封面 3:4", Width: 1242, Height: 1656},
	{Key: "xhs-1-1", Name: "小红书方图 1:1", Width: 1080, Height: 1080},
	{Key: "bili-16-9", Name: "B站封面 16:9", Width: 1920, Height: 1080},
	{Key: "wx-9-16", Name: "视频号竖屏 9:16", Width: 1080, Height: 1920},
	{Key: "mp-900-383", Name: "公众号头图 900:383", Width: 900, Height: 383},
}

// CanvasPresetByKey looks a canvas preset up.
func CanvasPresetByKey(key string) *CanvasPreset {
	for i := range CanvasPresets {
		if CanvasPresets[i].Key == key {
			p := CanvasPresets[i]
			return &p
		}
	}
	return nil
}

func canvasPresetKeys() []string {
	out := make([]string, len(CanvasPresets))
	for i, p := range CanvasPresets {
		out[i] = p.Key
	}
	return out
}

// TextPreset is a cover-style text look (黄底黑字, 黑描边 …).
type TextPreset struct {
	Key   string         `json:"key"`
	Name  string         `json:"name"`
	Style map[string]any `json:"style"`
}

// TextPresets are what applyTextPreset offers.
var TextPresets = []TextPreset{
	{Key: "plain-white", Name: "白色无衬线", Style: map[string]any{"fill": "#ffffff", "fontSize": 120.0, "fontWeight": 700.0, "lineHeight": 1.25}},
	{Key: "yellow-box", Name: "黄底黑字", Style: map[string]any{"fill": "#111111", "textBackgroundColor": "#ffd500", "fontSize": 128.0, "fontWeight": 800.0, "padding": 24.0, "lineHeight": 1.3}},
	{Key: "red-box", Name: "红底白字", Style: map[string]any{"fill": "#ffffff", "textBackgroundColor": "#e5322d", "fontSize": 120.0, "fontWeight": 700.0, "padding": 24.0, "lineHeight": 1.3}},
	{Key: "outline-black", Name: "黑描边", Style: map[string]any{"fill": "#ffffff", "stroke": "#000000", "strokeWidth": 8.0, "paintFirst": true, "fontSize": 132.0, "fontWeight": 900.0, "lineHeight": 1.2}},
	{Key: "soft-shadow", Name: "柔和投影", Style: map[string]any{"fill": "#ffffff", "shadow": "rgba(0,0,0,.55):24:4:6", "fontSize": 128.0, "fontWeight": 700.0, "lineHeight": 1.25}},
	{Key: "subtitle", Name: "小字副标题", Style: map[string]any{"fill": "rgba(255,255,255,.92)", "fontSize": 44.0, "fontWeight": 500.0, "charSpacing": 4.0, "lineHeight": 1.4}},
	{Key: "marker", Name: "荧光笔", Style: map[string]any{"fill": "#1a1a1a", "textBackgroundColor": "#c6f135", "fontSize": 104.0, "fontWeight": 700.0, "padding": 16.0, "lineHeight": 1.35}},
}

func textPresetByKey(key string) *TextPreset {
	for i := range TextPresets {
		if TextPresets[i].Key == key {
			return &TextPresets[i]
		}
	}
	return nil
}

func textPresetKeys() []string {
	out := make([]string, len(TextPresets))
	for i, p := range TextPresets {
		out[i] = p.Key
	}
	return out
}
