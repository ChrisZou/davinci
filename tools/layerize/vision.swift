// vision: Apple Vision OCR + face detection for one image, as JSON on stdout.
// Coordinates are image pixels, top-left origin.
import Foundation
import Vision
import AppKit

let path = CommandLine.arguments[1]
guard let img = NSImage(contentsOfFile: path),
      let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
  FileHandle.standardError.write("cannot read \(path)\n".data(using: .utf8)!); exit(1)
}
let W = Double(cg.width), H = Double(cg.height)
func pt(_ p: CGPoint) -> [Double] { [p.x * W, (1 - p.y) * H] }
func box(_ r: CGRect) -> [Double] { [r.minX * W, (1 - r.maxY) * H, r.width * W, r.height * H] }

let text = VNRecognizeTextRequest()
text.recognitionLevel = .accurate
text.recognitionLanguages = ["zh-Hans", "en-US"]
text.usesLanguageCorrection = false
text.minimumTextHeight = 0.012
let faces = VNDetectFaceRectanglesRequest()
let handler = VNImageRequestHandler(cgImage: cg, options: [:])
try handler.perform([text, faces])

var lines: [[String: Any]] = []
for obs in text.results ?? [] {
  guard let cand = obs.topCandidates(1).first else { continue }
  let s = cand.string
  var chars: [[String: Any]] = []
  var idx = s.startIndex
  while idx < s.endIndex {
    let next = s.index(after: idx)
    if let r = try? cand.boundingBox(for: idx..<next) {
      chars.append(["c": String(s[idx..<next]),
                    "quad": [pt(r.topLeft), pt(r.topRight), pt(r.bottomRight), pt(r.bottomLeft)]])
    }
    idx = next
  }
  lines.append(["text": s, "conf": cand.confidence, "box": box(obs.boundingBox),
                "quad": [pt(obs.topLeft), pt(obs.topRight), pt(obs.bottomRight), pt(obs.bottomLeft)],
                "chars": chars])
}
let fs = (faces.results ?? []).map { ["box": box($0.boundingBox), "conf": $0.confidence] as [String: Any] }
let out: [String: Any] = ["width": W, "height": H, "lines": lines, "faces": fs]
let data = try JSONSerialization.data(withJSONObject: out, options: [.sortedKeys])
FileHandle.standardOutput.write(data)
