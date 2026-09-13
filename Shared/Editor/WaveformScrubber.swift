import SwiftUI

/// Horizontally scrolling waveform beneath a fixed, centred selection window.
/// Scrolling the audio (with native momentum) picks where the snippet starts.
struct WaveformScrubber: View {
    @Bindable var model: SnippetEditorModel

    private let visibleSeconds: Double = 20
    private let barStep: CGFloat = 4
    private let barsPerSegment = 32

    @State private var position = ScrollPosition(x: 0)
    @State private var bars: [Float] = []
    @State private var tracker = ScrollTracker()

    var body: some View {
        GeometryReader { proxy in
            let width = proxy.size.width
            let height = proxy.size.height
            let pps = pointsPerSecond(width: width)
            let window = model.length * pps
            let leading = (width - window) / 2
            let trailing = width - leading - window
            let lane: CGFloat = model.lyrics.isEmpty ? 0 : 10

            ScrollView(.horizontal) {
                HStack(spacing: 0) {
                    Color.clear.frame(width: leading)
                    LazyHStack(spacing: 0) {
                        ForEach(Array(stride(from: 0, to: bars.count, by: barsPerSegment)), id: \.self) { first in
                            BarSegment(levels: Array(bars[first..<min(first + barsPerSegment, bars.count)]), step: barStep)
                                .frame(width: CGFloat(min(barsPerSegment, bars.count - first)) * barStep, height: height - lane)
                        }
                    }
                    .frame(width: model.duration * pps, height: height, alignment: .topLeading)
                    .overlay(alignment: .bottomLeading) {
                        if lane > 0 {
                            LyricLane(lines: model.lyrics, pointsPerSecond: pps).frame(height: lane)
                        }
                    }
                    Color.clear.frame(width: max(0, trailing))
                }
                .frame(height: height)
            }
            .scrollIndicators(.hidden)
            .scrollPosition($position)
            .onScrollGeometryChange(for: ScrollGeometry.self) { $0 } action: { _, geometry in
                tracker.geometry = geometry
                if tracker.pendingScroll {
                    applyPendingScroll(pps: pps)
                } else if pps > 0 {
                    model.start = (geometry.contentOffset.x / pps).clamped(to: 0...model.maxStart)
                }
            }
            .onScrollPhaseChange { old, new in
                switch new {
                case .interacting:
                    model.stopPreview()
                case .idle where old == .decelerating || old == .interacting:
                    Haptics.selection()
                    model.playSelection()
                default:
                    break
                }
            }
            .overlay { selectionOverlay(leading: leading, window: window) }
            .onChange(of: model.scrollRequest) { requestScroll(pps: pps) }
            .onChange(of: width) { requestScroll(pps: pps) }
            .task(id: BarKey(pps: pps, count: model.waveform.levels.count)) {
                bars = resampledBars(pps: pps)
            }
        }
        .accessibilityElement()
        .accessibilityLabel("Snippet start")
        .accessibilityValue("\(model.start.clock) to \((model.start + model.length).clock)")
        .accessibilityAdjustableAction { direction in
            let delta: TimeInterval = direction == .increment ? 1 : -1
            model.start = (model.start + delta).clamped(to: 0...model.maxStart)
            model.nudged()
        }
    }

    private func pointsPerSecond(width: CGFloat) -> CGFloat {
        guard model.duration > 0 else { return 0 }
        return width / min(visibleSeconds, max(model.duration, model.length))
    }

    private func requestScroll(pps: CGFloat) {
        tracker.pendingScroll = true
        applyPendingScroll(pps: pps)
    }

    /// Scrolls to `model.start` once the content is wide enough; until then offsets
    /// reported by the scroll view are layout artefacts and must not move the selection.
    private func applyPendingScroll(pps: CGFloat) {
        guard tracker.pendingScroll, pps > 0, let geometry = tracker.geometry else { return }
        let target = model.start * pps
        guard geometry.contentSize.width - geometry.containerSize.width + 0.5 >= target else { return }
        tracker.pendingScroll = false
        position.scrollTo(x: target)
    }

    private func resampledBars(pps: CGFloat) -> [Float] {
        let levels = model.waveform.levels
        guard pps > 0, !levels.isEmpty else { return [] }
        let count = Int((model.duration * pps / barStep).rounded(.down))
        let pointsPerBar = Double(barStep / pps) * Waveform.resolution
        return (0..<count).map { bar in
            let lower = Int(Double(bar) * pointsPerBar)
            let upper = max(lower + 1, Int(Double(bar + 1) * pointsPerBar))
            guard lower < levels.count else { return 0.05 }
            return levels[lower..<min(upper, levels.count)].max() ?? 0.05
        }
    }

    @ViewBuilder
    private func selectionOverlay(leading: CGFloat, window: CGFloat) -> some View {
        HStack(spacing: 0) {
            Rectangle().fill(.black.opacity(0.35)).frame(width: max(0, leading))
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(.white.opacity(0.10))
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(.white, lineWidth: 2.5)
                if model.isPreviewing {
                    Capsule()
                        .fill(.white)
                        .frame(width: 2.5)
                        .padding(.vertical, 6)
                        .offset(x: max(0, window * model.previewProgress - 1.25))
                        .shadow(color: .black.opacity(0.3), radius: 2)
                }
            }
            .frame(width: window)
            Rectangle().fill(.black.opacity(0.35))
        }
        .allowsHitTesting(false)
        .animation(.snappy, value: window)
    }
}

/// Per-frame scroll bookkeeping kept out of SwiftUI state to avoid re-rendering while scrolling.
private final class ScrollTracker {
    var geometry: ScrollGeometry?
    var pendingScroll = true
}

private struct BarKey: Hashable {
    let pps: CGFloat
    let count: Int
}

/// Where the vocals are: a dash under the waveform for each lyric line.
private struct LyricLane: View {
    let lines: [LyricLine]
    let pointsPerSecond: CGFloat

    var body: some View {
        Canvas { context, size in
            for line in lines {
                let rect = CGRect(
                    x: line.start * pointsPerSecond,
                    y: (size.height - 3) / 2,
                    width: max(3, (line.end - line.start) * pointsPerSecond - 2),
                    height: 3
                )
                context.fill(Path(roundedRect: rect, cornerRadius: 1.5), with: .color(.white.opacity(0.55)))
            }
        }
        .accessibilityHidden(true)
    }
}

private struct BarSegment: View {
    let levels: [Float]
    let step: CGFloat

    var body: some View {
        Canvas { context, size in
            let width = step * 0.62
            for (index, level) in levels.enumerated() {
                let height = max(width, size.height * 0.9 * CGFloat(level))
                let rect = CGRect(x: CGFloat(index) * step, y: (size.height - height) / 2, width: width, height: height)
                context.fill(Path(roundedRect: rect, cornerRadius: width / 2), with: .color(.white.opacity(0.9)))
            }
        }
    }
}
