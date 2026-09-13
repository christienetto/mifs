import SwiftUI

/// Somewhere a finished snippet can go, e.g. Messages or Telegram.
struct SendDestination: Identifiable {
    let id: String
    /// Short name used when several destinations share the send row ("Telegram").
    let name: String
    /// Full action title ("Send in Telegram"); also the button's accessibility label.
    let title: String
    let symbol: String
    let send: (Snippet) async throws -> Void
}

/// Pick the moment, preview it, send it. Hosted by both the app and the Messages extension.
struct SnippetEditorView: View {
    @State private var model: SnippetEditorModel
    let destinations: [SendDestination]

    @State private var sending: SendDestination.ID?
    @State private var sendError: String?

    init(track: Track, sendTitle: String = "Send", onSend: @escaping (Snippet) async throws -> Void) {
        self.init(track: track, destinations: [
            SendDestination(id: "send", name: sendTitle, title: sendTitle, symbol: "arrow.up.message.fill", send: onSend),
        ])
    }

    init(track: Track, destinations: [SendDestination]) {
        _model = State(initialValue: SnippetEditorModel(track: track))
        self.destinations = destinations
    }

    var body: some View {
        VStack(spacing: 18) {
            if model.lyrics.isEmpty {
                header
                    .frame(maxHeight: .infinity)
            } else {
                compactHeader
                LyricsPanel(
                    lines: model.lyrics,
                    selected: model.selectedLyrics,
                    playhead: model.playhead,
                    selectionStart: model.start,
                    onSelect: model.select
                )
                .frame(maxHeight: .infinity)
            }
            switch model.phase {
            case .loading:
                loadingTimeline
            case .ready:
                timeline
                controls
            case .failed(let message):
                failure(message)
            }
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 12)
        .padding(.top, 8)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background { ArtworkBackdrop(url: model.track.resolvedArtworkURL) }
        .foregroundStyle(.white)
        .environment(\.colorScheme, .dark)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .task { await model.load() }
        .onDisappear { model.stopPreview() }
        .alert("Couldn't send snippet", isPresented: Binding(get: { sendError != nil }, set: { if !$0 { sendError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(sendError ?? "")
        }
    }

    private var header: some View {
        VStack(spacing: 14) {
            ArtworkView(url: model.track.resolvedArtworkURL, cornerRadius: 18)
                .frame(maxWidth: 280, maxHeight: 280)
                .shadow(color: .black.opacity(0.35), radius: 24, y: 12)
                .layoutPriority(-1)
            VStack(spacing: 4) {
                Text(model.track.title)
                    .font(.title3.weight(.bold))
                    .multilineTextAlignment(.center)
                    .lineLimit(2)
                Text(model.track.artist)
                    .font(.body)
                    .opacity(0.75)
                    .lineLimit(1)
            }
        }
    }

    /// Makes room for lyrics.
    private var compactHeader: some View {
        HStack(spacing: 12) {
            ArtworkView(url: model.track.resolvedArtworkURL, cornerRadius: 10)
                .frame(width: 56)
                .shadow(color: .black.opacity(0.3), radius: 8, y: 4)
            VStack(alignment: .leading, spacing: 2) {
                Text(model.track.title)
                    .font(.headline)
                    .lineLimit(1)
                Text(model.track.artist)
                    .font(.subheadline)
                    .opacity(0.75)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
        }
    }

    private var caption: String {
        switch model.track.kind {
        case .catalog: "Drag the waveform to pick the moment · from Apple Music’s preview"
        case .server where !model.lyrics.isEmpty: "Drag the waveform or tap a lyric to pick the moment"
        case .server, .file: "Drag the waveform to pick the moment"
        }
    }

    private var loadingMessage: String {
        switch model.track.kind {
        case .catalog: "Loading preview…"
        case .server: "Loading song…"
        case .file: "Reading audio…"
        }
    }

    private var timeline: some View {
        VStack(spacing: 10) {
            HStack {
                Text(model.start.clock)
                Spacer()
                Text("\(model.length.shortSeconds) snippet").fontWeight(.semibold).opacity(1)
                Spacer()
                Text((model.start + model.length).clock)
            }
            .font(.footnote.monospacedDigit())
            .opacity(0.85)

            WaveformScrubber(model: model)
                .frame(height: 92)

            OverviewBar(start: model.start, length: model.length, duration: model.duration)
                .frame(height: 4)

            Text(caption)
                .font(.caption)
                .opacity(0.6)
                .multilineTextAlignment(.center)
        }
    }

    private var loadingTimeline: some View {
        VStack(spacing: 12) {
            ProgressView().tint(.white)
            Text(loadingMessage)
                .font(.footnote)
                .opacity(0.7)
        }
        .frame(height: 180)
    }

    private var controls: some View {
        VStack(spacing: 16) {
            LengthPicker(options: model.availableLengths, selection: model.length) { model.setLength($0) }

            HStack(spacing: 14) {
                PlayButton(state: model.previewState, progress: model.previewProgress, size: 54) {
                    Haptics.tap()
                    model.togglePreview()
                }

                HStack(spacing: 10) {
                    ForEach(destinations) { destination in
                        sendButton(destination)
                    }
                }
            }
        }
    }

    private func sendButton(_ destination: SendDestination) -> some View {
        let label = destinations.count > 1 ? destination.name : destination.title
        return Button {
            Task { await send(to: destination) }
        } label: {
            ViewThatFits(in: .horizontal) {
                sendLabel(label, symbol: destination.symbol, isSending: sending == destination.id)
                sendLabel(label, symbol: nil, isSending: sending == destination.id)
            }
            .frame(maxWidth: .infinity, minHeight: 54)
            .background(.white, in: .capsule)
            .foregroundStyle(.black)
        }
        .buttonStyle(.plain)
        .disabled(model.isSaving || sending != nil)
        .accessibilityLabel(destination.title)
        .accessibilityHint("Sends a \(Int(model.length)) second snippet")
    }

    private func sendLabel(_ text: String, symbol: String?, isSending: Bool) -> some View {
        HStack(spacing: 8) {
            if isSending {
                ProgressView().tint(.black)
            } else if let symbol {
                Image(systemName: symbol)
            }
            Text(text).fontWeight(.semibold).lineLimit(1)
        }
        .padding(.horizontal, 12)
    }

    private func failure(_ message: String) -> some View {
        VStack(spacing: 14) {
            Image(systemName: "exclamationmark.triangle.fill").font(.title)
            Text(message).font(.subheadline).multilineTextAlignment(.center).opacity(0.85)
            Button("Try Again") { Task { await model.load() } }
                .buttonStyle(.bordered)
                .tint(.white)
        }
        .frame(height: 200)
    }

    private func send(to destination: SendDestination) async {
        sending = destination.id
        defer { sending = nil }
        do {
            let snippet = try await model.makeSnippet()
            try await destination.send(snippet)
        } catch is CancellationError {
        } catch {
            sendError = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
        }
    }
}

private struct OverviewBar: View {
    let start: TimeInterval
    let length: TimeInterval
    let duration: TimeInterval

    var body: some View {
        GeometryReader { proxy in
            let width = proxy.size.width
            let fraction = duration > 0 ? width / duration : 0
            ZStack(alignment: .leading) {
                Capsule().fill(.white.opacity(0.2))
                Capsule()
                    .fill(.white)
                    .frame(width: max(4, length * fraction))
                    .offset(x: start * fraction)
            }
        }
        .accessibilityHidden(true)
    }
}

private struct LengthPicker: View {
    let options: [TimeInterval]
    let selection: TimeInterval
    let onSelect: (TimeInterval) -> Void

    var body: some View {
        HStack(spacing: 8) {
            ForEach(options, id: \.self) { option in
                let selected = abs(option - selection) < 0.01
                Button {
                    onSelect(option)
                } label: {
                    Text(option.shortSeconds)
                        .font(.subheadline.weight(.semibold).monospacedDigit())
                        .frame(maxWidth: .infinity, minHeight: 36)
                        .background(selected ? AnyShapeStyle(.white) : AnyShapeStyle(.white.opacity(0.14)), in: .capsule)
                        .foregroundStyle(selected ? .black : .white)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(Int(option)) seconds")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .animation(.snappy(duration: 0.2), value: selection)
    }
}
