import SwiftUI

struct TrackRow: View {
    let track: Track
    var rank: Int?

    @State private var player = SnippetPlayer.shared

    var body: some View {
        HStack(spacing: 12) {
            if let rank {
                Text("\(rank)")
                    .font(.subheadline.monospacedDigit().weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(minWidth: 22)
            }
            ArtworkView(url: track.thumbnailURL ?? Track.artwork(track.artworkURL, size: 200), cornerRadius: 8)
                .frame(width: 52)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(track.title).font(.body.weight(.medium)).lineLimit(1)
                    if track.isExplicit {
                        Image(systemName: "e.square.fill")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("Explicit")
                    }
                }
                Text(track.artist).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 8)
            if let preview = track.previewURL {
                let id = "row-\(track.id)"
                Button {
                    Haptics.tap()
                    player.toggle(id: id, url: preview, start: track.highlightStart ?? 0, duration: 30)
                } label: {
                    Image(systemName: player.isActive(id) ? "stop.circle.fill" : "play.circle")
                        .font(.title2)
                        .foregroundStyle(Color.accentColor)
                        .contentTransition(.symbolEffect(.replace))
                }
                .buttonStyle(.borderless)
                .accessibilityLabel(player.isActive(id) ? "Stop preview" : "Play preview")
            }
        }
        .contentShape(.rect)
    }
}
