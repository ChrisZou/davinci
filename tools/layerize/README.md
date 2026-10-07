# layerize：把一张平面封面拆成 davinci 图层

```bash
swiftc -O vision.swift -o bin/vision          # 一次：Apple Vision 的 OCR / 人脸检测
pip install rembg simple-lama-inpainting opencv-python pillow numpy fonttools
python3 layerize.py cover.png out/ [--crop x0,y0,x1,y1] [--fix fixes.json] [--text-only]
python3 import_cover.py out/ cover.png "项目名"   # 画板 1 = 分层设计稿，画板 2 = 原图
```

- **文字**：Vision OCR（行 + 逐字框）→ 按颜色把字从背景里分出来（填充色、描边色和粗细，
  标签底色框上的字也能分）→ 把识别出的字用每个候选字体按实测字高/字宽画出来，与原图字形比对
  重合度选字体（含 Chrome 的伪斜体）→ 位置按 `calib.json`（每个字体在 davinci 里实测的
  基线偏移）换算。
- **斜着走的标题**：Vision 的行框常常是水平的，所以角度还要从字本身量——逐字的墨迹中心
  （Theil–Sen 拟合）和横笔的走向；在这几个角度上重读这一行，取还原得最好的那个。整行斜向上
  有两种做法都会试：整体旋转（竖笔跟着歪）和纵向斜切 `skewY`（竖笔不歪）；再加上斜体，
  三者靠比对横笔、竖笔的方向（`stroke_angles`）区分，不只看重合度。
- **人物**：Vision 检测到人脸才抠，rembg `birefnet-portrait`，在去掉文字后的图上抠。
- **元素**：人物挖掉后再跑 `birefnet-general`，两轮，图标 / 设备 / 吉祥物依次浮出来。
- **背景**：LaMa 补全被拿走的部分。

`fixes.json`（人工复核后给的修正，可选）：

```json
{"lines": {"OCR 读错的行": "正确文字"}, "drop": ["照片里的字，不要单独成层"], "drop_runs": ["行里的某一段，同上"],
 "add": [{"text": "夯", "box": [30, 20, 340, 290], "behind": true}]}
```

`drop` / `drop_runs` 的字留在画面里：既不成层，也不擦。`--text-only` 只跑文字部分（几秒），
写出 `report.json` 和 `text.json`，改了文字逻辑后拿来对比各张封面。

`fonts.py` 是候选字体表（字体文件放 `~/Library/Fonts`；庞门正道粗书体等免费商用字体可从
[wordshub/free-font](https://github.com/wordshub/free-font) 取）；换了字体后用一张每个字体各写一个
“国”“H”的校准页重新生成 `calib.json`（`python3 calibrate.py`，需要 davinci 服务在跑）。
