/** Copy the layer's current source asset at its native resolution, including alpha. */
export async function copyImageToClipboard(url: string): Promise<void> {
  if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
    throw new Error('当前浏览器不支持复制图片')
  }
  const png = imagePNG(url)
  // Start the clipboard write during the click, before loading/decoding finishes.
  // Observe loading failures even if clipboard access is denied immediately.
  void png.catch(() => {})
  await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })])
}

async function imagePNG(url: string): Promise<Blob> {
  const response = await fetch(url)
  if (!response.ok) throw new Error(`图片加载失败（${response.status}）`)
  const blob = await response.blob()
  if (blob.type === 'image/png') return blob

  // Clipboard image support is most consistent with PNG; convert JPEG/WebP/etc.
  const image = await createImageBitmap(blob)
  try {
    const canvas = document.createElement('canvas')
    canvas.width = image.width
    canvas.height = image.height
    const context = canvas.getContext('2d')
    if (!context) throw new Error('无法读取图片')
    context.drawImage(image, 0, 0)
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((png) => png ? resolve(png) : reject(new Error('图片转换失败')), 'image/png')
    })
  } finally {
    image.close()
  }
}
