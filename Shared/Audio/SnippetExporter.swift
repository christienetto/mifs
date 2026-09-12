import AVFoundation

nonisolated enum SnippetFades {
    static let fadeIn: TimeInterval = 0.08
    static let fadeOut: TimeInterval = 0.4

    /// Short fade-in/out so clips never start or stop with a click.
    static func audioMix(for track: AVAssetTrack, start: TimeInterval, duration: TimeInterval) -> AVAudioMix {
        let parameters = AVMutableAudioMixInputParameters(track: track)
        let fadeOut = min(Self.fadeOut, duration / 4)
        parameters.setVolumeRamp(
            fromStartVolume: 0, toEndVolume: 1,
            timeRange: CMTimeRange(start: .seconds(start), duration: .seconds(fadeIn))
        )
        parameters.setVolumeRamp(
            fromStartVolume: 1, toEndVolume: 0,
            timeRange: CMTimeRange(start: .seconds(start + duration - fadeOut), duration: .seconds(fadeOut))
        )
        let mix = AVMutableAudioMix()
        mix.inputParameters = [parameters]
        return mix
    }
}

/// Cuts a clip from audio the user owns into a standalone AAC file in the app group.
nonisolated enum SnippetExporter {
    enum Failure: LocalizedError {
        case cannotExport

        var errorDescription: String? { "MIFS couldn't create the audio clip. Try a different song." }
    }

    /// Returns the app-group relative path of the exported `.m4a`.
    @concurrent
    static func export(
        from source: URL,
        start: TimeInterval,
        duration: TimeInterval,
        title: String,
        artist: String,
        artwork: Data?
    ) async throws -> String {
        let asset = AVURLAsset(url: source)
        guard let track = try await asset.loadTracks(withMediaType: .audio).first,
              let session = AVAssetExportSession(asset: asset, presetName: AVAssetExportPresetAppleM4A)
        else { throw Failure.cannotExport }

        session.timeRange = CMTimeRange(start: .seconds(start), duration: .seconds(duration))
        session.audioMix = SnippetFades.audioMix(for: track, start: start, duration: duration)
        session.metadata = metadata(title: title, artist: artist, artwork: artwork)

        let relative = try AppGroup.newFile(in: "Clips", extension: "m4a")
        let destination = AppGroup.url(for: relative)
        do {
            try await session.export(to: destination, as: .m4a)
        } catch {
            try? FileManager.default.removeItem(at: destination)
            throw error
        }
        return relative
    }

    private static func metadata(title: String, artist: String, artwork: Data?) -> [AVMetadataItem] {
        func item(_ identifier: AVMetadataIdentifier, _ value: any NSCopying & NSObjectProtocol, dataType: String? = nil) -> AVMetadataItem {
            let item = AVMutableMetadataItem()
            item.identifier = identifier
            item.value = value
            item.extendedLanguageTag = "und"
            if let dataType { item.dataType = dataType }
            return item
        }
        var items = [
            item(.commonIdentifierTitle, title as NSString),
            item(.commonIdentifierArtist, artist as NSString),
            item(.commonIdentifierSoftware, "MIFS" as NSString),
        ]
        if let artwork {
            items.append(item(.commonIdentifierArtwork, artwork as NSData, dataType: kCMMetadataBaseDataType_JPEG as String))
        }
        return items
    }
}

extension CMTime {
    nonisolated static func seconds(_ value: TimeInterval) -> CMTime {
        CMTime(seconds: value, preferredTimescale: 44_100)
    }
}
