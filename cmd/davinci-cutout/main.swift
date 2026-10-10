// davinci-cutout: cuts the subject out of a picture with the subject lifting
// built into macOS (Vision, as in Photos). davinci uses it for "去除背景" when
// rembg is not installed — the app ships it, so cutting needs no Python and no
// model download.
//
//   davinci-cutout <in> <out.png>
//
// The PNG keeps the input's pixel size; everything but the subject is
// transparent. Exit 2: no subject found.
import CoreImage
import Foundation
import Vision

func fail(_ msg: String, _ code: Int32 = 1) -> Never {
    FileHandle.standardError.write((msg + "\n").data(using: .utf8)!)
    exit(code)
}

let args = CommandLine.arguments
guard args.count == 3 else { fail("usage: davinci-cutout <in> <out.png>") }
let input = URL(fileURLWithPath: args[1])
let output = URL(fileURLWithPath: args[2])

guard #available(macOS 14.0, *) else { fail("需要 macOS 14 或更新的系统") }
let request = VNGenerateForegroundInstanceMaskRequest()
let handler = VNImageRequestHandler(url: input, options: [:])
do {
    try handler.perform([request])
} catch {
    fail("读不了这张图：\(error.localizedDescription)")
}
guard let result = request.results?.first, !result.allInstances.isEmpty else {
    fail("没找到可以抠出来的主体", 2)
}
let cut: CVPixelBuffer
do {
    cut = try result.generateMaskedImage(ofInstances: result.allInstances, from: handler, croppedToInstancesExtent: false)
} catch {
    fail("抠图失败：\(error.localizedDescription)")
}
let image = CIImage(cvPixelBuffer: cut)
let space = CGColorSpace(name: CGColorSpace.sRGB)!
do {
    try CIContext().writePNGRepresentation(of: image, to: output, format: .RGBA8, colorSpace: space)
} catch {
    fail("写不出 PNG：\(error.localizedDescription)")
}
