import SwiftUI

enum Theme {
    static let violet = Color(red: 0.42, green: 0.30, blue: 1.0)
    static let pink = Color(red: 1.0, green: 0.31, blue: 0.60)
    static let brandGradient = LinearGradient(colors: [violet, pink], startPoint: .topLeading, endPoint: .bottomTrailing)
    static let fallbackTint = UIColor(red: 0.24, green: 0.18, blue: 0.45, alpha: 1)
    static let telegram = Color(red: 0.16, green: 0.63, blue: 0.87)
}

extension TimeInterval {
    /// "0:07", "3:42".
    var clock: String {
        let total = Int(self.rounded(.down))
        return String(format: "%d:%02d", total / 60, total % 60)
    }

    /// "10s", "12.5s".
    var shortSeconds: String {
        let rounded = (self * 2).rounded() / 2
        return rounded == rounded.rounded() ? "\(Int(rounded))s" : String(format: "%.1fs", rounded)
    }
}

enum Haptics {
    static func selection() { UISelectionFeedbackGenerator().selectionChanged() }
    static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
    static func success() { UINotificationFeedbackGenerator().notificationOccurred(.success) }
}
