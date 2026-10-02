# layerize：把一张平面封面拆成 davinci 图层

```bash
swiftc -O vision.swift -o bin/vision          # 一次：Apple Vision 的 OCR / 人脸检测
pip install rembg simple-lama-inpainting opencv-python pillow numpy fonttools
python3 layerize.py cover.png out/ [--crop x0,y0,x1,y1] [--fix fixes.json]
python3 import_cover.py out/ cover.png "项目名"   # 画板 1 = 分层设计稿，画板 2 = 原图
```

- **文字**：Vision OCR（行 + 逐字框）→ 按颜色把字从背景里分出来（填充色、描边色和粗细，
  标签底色框上的字也能分）→ 把识别出的字用每个候选字体按实测字高/字宽画出来，与原图字形比对
  重合度选字体（含 Chrome 的伪斜体）→ 位置按 `calib.json`（每个字体在 davinci 里实测的
  基线偏移）换算。
- **人物**：Vision 检测到人脸才抠，rembg `birefnet-portrait`，在去掉文字后的图上抠。
- **元素**：人物挖掉后再跑 `birefnet-general`，两轮，图标 / 设备 / 吉祥物依次浮出来。
- **背景**：LaMa 补全被拿走的部分。

`fixes.json`（人工复核后给的修正，可选）：

```json
{"lines": {"OCR 读错的行": "正确文字"}, "drop": ["照片里的字，不要单独成层"],
 "add": [{"text": "夯", "box": [30, 20, 340, 290], "behind": true}]}
```

`fonts.py` 是候选字体表；换了字体后用一张每个字体各写一个“国”“H”的校准页重新生成 `calib.json`。
