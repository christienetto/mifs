import CoreImage
import SwiftUI
import UIKit

/// Loads and caches artwork (remote catalog art or files in the app group).
final class ArtworkLoader {
    static let shared = ArtworkLoader()

    private let images = NSCache<NSURL, UIImage>()
    private let tints = NSCache<NSURL, UIColor>()

    func cached(_ url: URL) -> UIImage? { images.object(forKey: url as NSURL) }

    func image(for url: URL) async -> UIImage? {
        if let cached = cached(url) { return cached }
        guard let image = await Self.fetch(url) else { return nil }
        images.setObject(image, forKey: url as NSURL)
        return image
    }

    /// A dark, saturated colour sampled from the artwork, suitable behind white text.
    func tint(for url: URL) async -> UIColor? {
        if let tint = tints.object(forKey: url as NSURL) { return tint }
        guard let image = await image(for: url), let tint = await Self.averageColor(of: image) else { return nil }
        tints.setObject(tint, forKey: url as NSURL)
        return tint
    }

    @concurrent
    private static func fetch(_ url: URL) async -> UIImage? {
        let data: Data?
        if url.isFileURL {
            data = try? Data(contentsOf: url)
        } else {
            data = try? await URLSession.shared.data(from: url).0
        }
        guard let data, let image = UIImage(data: data) else { return nil }
        return await image.byPreparingForDisplay() ?? image
    }

    @concurrent
    private static func averageColor(of image: UIImage) async -> UIColor? {
        guard let input = CIImage(image: image) else { return nil }
        let filter = CIFilter(name: "CIAreaAverage", parameters: [
            kCIInputImageKey: input,
            kCIInputExtentKey: CIVector(cgRect: input.extent),
        ])
        guard let output = filter?.outputImage else { return nil }
        var pixel = [UInt8](repeating: 0, count: 4)
        CIContext(options: [.workingColorSpace: NSNull()]).render(
            output, toBitmap: &pixel, rowBytes: 4,
            bounds: CGRect(x: 0, y: 0, width: 1, height: 1), format: .RGBA8, colorSpace: nil
        )
        let color = UIColor(red: CGFloat(pixel[0]) / 255, green: CGFloat(pixel[1]) / 255, blue: CGFloat(pixel[2]) / 255, alpha: 1)
        var hue: CGFloat = 0, saturation: CGFloat = 0, brightness: CGFloat = 0, alpha: CGFloat = 0
        color.getHue(&hue, saturation: &saturation, brightness: &brightness, alpha: &alpha)
        return UIColor(
            hue: hue,
            saturation: min(max(saturation * 1.15, 0.25), 0.85),
            brightness: min(max(brightness * 0.8, 0.22), 0.45),
            alpha: 1
        )
    }
}

struct ArtworkView: View {
    let url: URL?
    var cornerRadius: CGFloat = 8

    @State private var image: UIImage?

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
                    .transition(.opacity)
            } else {
                Rectangle()
                    .fill(Theme.brandGradient.opacity(0.35))
                    .overlay {
                        Image(systemName: "music.note")
                            .font(.title2.weight(.semibold))
                            .foregroundStyle(.white.opacity(0.85))
                    }
            }
        }
        .aspectRatio(1, contentMode: .fit)
        .clipShape(.rect(cornerRadius: cornerRadius, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                .strokeBorder(.primary.opacity(0.08), lineWidth: 0.5)
        }
        .accessibilityHidden(true)
        .task(id: url) {
            guard let url else { image = nil; return }
            if let cached = ArtworkLoader.shared.cached(url) { image = cached; return }
            let loaded = await ArtworkLoader.shared.image(for: url)
            withAnimation(.easeOut(duration: 0.2)) { image = loaded }
        }
    }
}

/// Blurred artwork wash used behind players and the editor.
struct ArtworkBackdrop: View {
    let url: URL?
    @State private var tint: Color = Color(Theme.fallbackTint)

    var body: some View {
        // Overlays never affect layout, so the square artwork can't widen the screen.
        tint
            .overlay {
                ArtworkView(url: url, cornerRadius: 0)
                    .scaledToFill()
                    .blur(radius: 60, opaque: true)
                    .opacity(0.55)
            }
            .overlay {
                LinearGradient(colors: [.clear, .black.opacity(0.55)], startPoint: .top, endPoint: .bottom)
            }
            .clipped()
            .ignoresSafeArea()
        .task(id: url) {
            guard let url, let color = await ArtworkLoader.shared.tint(for: url) else { return }
            withAnimation(.easeInOut(duration: 0.4)) { tint = Color(color) }
        }
    }
}
