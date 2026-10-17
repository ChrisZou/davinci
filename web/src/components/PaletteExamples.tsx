import type { PaletteColors } from '../api'

function luminance(hex: string) {
  const values = (hex.match(/[a-f\d]{2}/gi) ?? ['00', '00', '00']).map((n) => {
    const v = parseInt(n, 16) / 255
    return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4
  })
  return .2126 * values[0] + .7152 * values[1] + .0722 * values[2]
}

/** Keep the supplied colours. Use an underline if a filled marker would hide text. */
export function highlightStyle(palette: PaletteColors) {
  const a = luminance(palette.text), b = luminance(palette.highlight)
  return (Math.max(a, b) + .05) / (Math.min(a, b) + .05) >= 4.5
    ? { color: palette.text, backgroundColor: palette.highlight }
    : { color: palette.text, backgroundColor: 'transparent', textDecoration: 'underline', textDecorationColor: palette.highlight, textDecorationThickness: '.14em', textUnderlineOffset: '.12em' }
}

export function PaletteExamples({ palette: p }: { palette: PaletteColors }) {
  return (
    <section aria-label="示例设计" className="palette-examples">
      <h2 className="m-0 font-serif text-lg font-black">示例设计</h2>
      <figure className="palette-example palette-slide">
        <div className="palette-art" style={{ background: p.bg, color: p.text }} aria-label="16:9 幻灯片">
          <div className="slide-content">
            <span className="sample-dot" style={{ background: p.accent }} />
            <h3>让<mark style={highlightStyle(p)}>灵感</mark>成为日常</h3>
            <p style={{ color: p.text2 }}>从一抹色彩，开始新的表达。</p>
            <span className="sample-page" style={{ color: p.accent }}>01</span>
          </div>
        </div>
        <figcaption>幻灯片 <span>16:9</span></figcaption>
      </figure>
      <figure className="palette-example palette-poster">
        <div className="palette-art" style={{ background: p.bg, color: p.text }} aria-label="3:4 竖版封面">
          <div className="poster-content">
            <span className="poster-kicker" style={{ color: p.text2 }}>生活观察 / 第七期</span>
            <h3>留一点<br /><mark style={highlightStyle(p)}>想象</mark>的空间</h3>
            <div className="poster-shape" style={{ background: p.highlight }}><i style={{ background: p.bg }} /></div>
            <p style={{ color: p.text2 }}>慢下来，发现平凡里的美。</p>
            <span className="poster-foot"><i style={{ background: p.accent }} />色彩手记 · 2026</span>
          </div>
        </div>
        <figcaption>竖版封面 <span>3:4</span></figcaption>
      </figure>
      <figure className="palette-example palette-social">
        <div className="palette-art" style={{ background: p.bg, color: p.text }} aria-label="1:1 社交卡片">
          <div className="social-content">
            <span style={{ color: p.text2 }}>每周灵感</span>
            <span className="social-quote" style={{ color: p.highlight }}>“</span>
            <h3>好的设计，<br />让日常<mark style={highlightStyle(p)}>有光</mark>。</h3>
            <p style={{ color: p.text2 }}>记录值得分享的每一个瞬间</p>
            <div className="social-foot"><i style={{ background: p.accent }} /><span style={{ color: p.text2 }}>设计与生活</span></div>
          </div>
        </div>
        <figcaption>社交卡片 <span>1:1</span></figcaption>
      </figure>
    </section>
  )
}
