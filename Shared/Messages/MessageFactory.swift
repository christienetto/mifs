import Messages
import UIKit

enum MessageFactory {
    /// A MIFS bubble for a catalog snippet. Messages draws it natively from a card image and captions;
    /// tapping it opens the MIFS player. Devices without MIFS open the song on music.apple.com.
    static func message(for snippet: Snippet) async -> MSMessage? {
        guard let url = SnippetLink.url(for: snippet) else { return nil }

        var artwork: UIImage?
        var tint = Theme.fallbackTint
        if let artworkURL = snippet.track.artworkURL {
            artwork = await ArtworkLoader.shared.image(for: artworkURL)
            tint = await ArtworkLoader.shared.tint(for: artworkURL) ?? tint
        }

        let layout = MSMessageTemplateLayout()
        layout.image = card(artwork: artwork, tint: tint, waveform: snippet.waveform, duration: snippet.duration)
        layout.caption = snippet.track.title
        layout.subcaption = snippet.track.artist
        layout.trailingSubcaption = "▶︎ \(snippet.duration.clock)"

        let message = MSMessage()
        message.layout = layout
        message.url = url
        message.summaryText = "🎵 \(snippet.track.title) – \(snippet.track.artist)"
        message.accessibilityLabel = "Music snippet: \(snippet.track.title) by \(snippet.track.artist), \(Int(snippet.duration)) seconds"
        return message
    }

    /// 3:2 card: artwork on an album-tinted ground, with the snippet's real waveform and a play badge.
    static func card(artwork: UIImage?, tint: UIColor, waveform: [Float], duration: TimeInterval) -> UIImage {
        let size = CGSize(width: 900, height: 600)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true
        return UIGraphicsImageRenderer(size: size, format: format).image { context in
            let cg = context.cgContext
            let space = CGColorSpaceCreateDeviceRGB()

            let colors = [tint.adjusted(brightness: 1.25).cgColor, tint.adjusted(brightness: 0.7).cgColor] as CFArray
            if let gradient = CGGradient(colorsSpace: space, colors: colors, locations: [0, 1]) {
                cg.drawLinearGradient(gradient, start: .zero, end: CGPoint(x: size.width, y: size.height), options: [])
            }

            // Artwork with a soft shadow.
            let art = CGRect(x: 60, y: 90, width: 420, height: 420)
            cg.saveGState()
            cg.setShadow(offset: CGSize(width: 0, height: 14), blur: 40, color: UIColor.black.withAlphaComponent(0.45).cgColor)
            UIBezierPath(roundedRect: art, cornerRadius: 34).fill()
            cg.restoreGState()
            cg.saveGState()
            UIBezierPath(roundedRect: art, cornerRadius: 34).addClip()
            if let artwork {
                artwork.draw(in: art)
            } else {
                UIColor.white.withAlphaComponent(0.15).setFill()
                UIRectFill(art)
            }
            cg.restoreGState()

            // Waveform.
            let wave = CGRect(x: 530, y: 150, width: 310, height: 190)
            let bars = waveform.isEmpty ? Array(repeating: Float(0.4), count: 24) : waveform
            let step = wave.width / CGFloat(bars.count)
            let barWidth = max(3, step * 0.55)
            UIColor.white.setFill()
            for (index, level) in bars.enumerated() {
                let height = max(barWidth, wave.height * CGFloat(level))
                let bar = CGRect(x: wave.minX + CGFloat(index) * step, y: wave.midY - height / 2, width: barWidth, height: height)
                UIBezierPath(roundedRect: bar, cornerRadius: barWidth / 2).fill()
            }

            // Play badge + duration.
            let badge = CGRect(x: 530, y: 390, width: 104, height: 104)
            UIColor.white.setFill()
            UIBezierPath(ovalIn: badge).fill()
            let triangle = UIBezierPath()
            triangle.move(to: CGPoint(x: badge.minX + 40, y: badge.minY + 30))
            triangle.addLine(to: CGPoint(x: badge.minX + 40, y: badge.maxY - 30))
            triangle.addLine(to: CGPoint(x: badge.maxX - 28, y: badge.midY))
            triangle.close()
            tint.adjusted(brightness: 0.8).setFill()
            triangle.fill()

            let label = NSAttributedString(string: duration.clock, attributes: [
                .font: UIFont.monospacedDigitSystemFont(ofSize: 52, weight: .bold),
                .foregroundColor: UIColor.white,
            ])
            label.draw(at: CGPoint(x: badge.maxX + 28, y: badge.midY - label.size().height / 2))
        }
    }
}

private extension UIColor {
    func adjusted(brightness factor: CGFloat) -> UIColor {
        var hue: CGFloat = 0, saturation: CGFloat = 0, brightness: CGFloat = 0, alpha: CGFloat = 0
        guard getHue(&hue, saturation: &saturation, brightness: &brightness, alpha: &alpha) else { return self }
        return UIColor(hue: hue, saturation: saturation, brightness: min(1, brightness * factor), alpha: alpha)
    }
}
