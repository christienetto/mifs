import SwiftUI

/// Synced lyrics beside the timeline: lines inside the selection are lit, the line being
/// heard is emphasised, and tapping a line moves the snippet there.
struct LyricsPanel: View {
    let lines: [LyricLine]
    let selected: [LyricLine]
    /// Song time being heard, while previewing.
    let playhead: TimeInterval?
    /// Start of the selection; keeps the panel scrolled near it in instrumental passages.
    let selectionStart: TimeInterval
    let onSelect: (LyricLine) -> Void

    var body: some View {
        let sung = playhead.flatMap { lines.index(at: $0) }
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    ForEach(lines.indices, id: \.self) { index in
                        let line = lines[index]
                        let isSelected = selected.contains(line)
                        Button { onSelect(line) } label: {
                            Text(line.text)
                                .font(.title3.weight(.bold))
                                .multilineTextAlignment(.leading)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .opacity(isSelected ? (sung == nil || sung == index ? 1 : 0.7) : 0.32)
                                .scaleEffect(sung == index ? 1.04 : 1, anchor: .leading)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .id(index)
                        .accessibilityIdentifier("lyric-line")
                        .accessibilityHint("Starts the snippet at this line")
                        .accessibilityAddTraits(isSelected ? .isSelected : [])
                    }
                }
                .padding(.vertical, 36)
                .animation(.smooth(duration: 0.25), value: sung)
                .animation(.smooth(duration: 0.2), value: selected)
            }
            .scrollIndicators(.hidden)
            .mask {
                LinearGradient(
                    stops: [.init(color: .clear, location: 0), .init(color: .black, location: 0.14),
                            .init(color: .black, location: 0.86), .init(color: .clear, location: 1)],
                    startPoint: .top, endPoint: .bottom
                )
            }
            .onAppear { proxy.scrollTo(anchorIndex, anchor: .center) }
            .onChange(of: anchorIndex) { _, index in
                withAnimation(.smooth) { proxy.scrollTo(index, anchor: .center) }
            }
        }
    }

    /// The first selected line, or else the next line after the selection starts.
    private var anchorIndex: Int {
        if let first = selected.first, let index = lines.firstIndex(of: first) { return index }
        return lines.firstIndex { $0.start >= selectionStart } ?? max(0, lines.count - 1)
    }
}
