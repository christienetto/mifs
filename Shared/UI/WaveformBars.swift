import SwiftUI

/// Rounded bars with a played/unplayed split, like Messages audio bubbles.
struct WaveformBars: View {
    let levels: [Float]
    var progress: Double = 0
    var played: Color = .white
    var unplayed: Color = .white.opacity(0.35)
    var spacing: CGFloat = 2

    var body: some View {
        Canvas { context, size in
            guard !levels.isEmpty else { return }
            let count = CGFloat(levels.count)
            let barWidth = max(1, (size.width - spacing * (count - 1)) / count)
            let playedX = size.width * progress
            for (index, level) in levels.enumerated() {
                let x = CGFloat(index) * (barWidth + spacing)
                let height = max(barWidth, size.height * CGFloat(level))
                let rect = CGRect(x: x, y: (size.height - height) / 2, width: barWidth, height: height)
                let color = x + barWidth / 2 <= playedX ? played : unplayed
                context.fill(Path(roundedRect: rect, cornerRadius: barWidth / 2), with: .color(color))
            }
        }
        .accessibilityHidden(true)
    }
}
