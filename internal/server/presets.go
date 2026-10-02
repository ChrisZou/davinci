package server

// CanvasPreset is a stock canvas size aimed at the creator platforms 创哥 posts to.
type CanvasPreset struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// canvasPresets are the sizes offered by `davinci new` and the editor's
// "new project" dialog. Keys are stable; the AI refers to them by key.
var canvasPresets = []CanvasPreset{
	{Key: "xhs-3-4", Name: "小红书封面 3:4", Width: 1242, Height: 1656},
	{Key: "xhs-1-1", Name: "小红书方图 1:1", Width: 1080, Height: 1080},
	{Key: "bili-16-9", Name: "B站封面 16:9", Width: 1920, Height: 1080},
	{Key: "wx-9-16", Name: "视频号竖屏 9:16", Width: 1080, Height: 1920},
	{Key: "mp-900-383", Name: "公众号头图 900:383", Width: 900, Height: 383},
}

// Preset looks up a canvas preset by key, returning nil when unknown.
func Preset(key string) *CanvasPreset {
	for i := range canvasPresets {
		if canvasPresets[i].Key == key {
			p := canvasPresets[i]
			return &p
		}
	}
	return nil
}

// Presets returns every canvas preset.
func Presets() []CanvasPreset {
	out := make([]CanvasPreset, len(canvasPresets))
	copy(out, canvasPresets)
	return out
}
