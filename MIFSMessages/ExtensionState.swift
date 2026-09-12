import Foundation
import Observation

@Observable
final class ExtensionState {
    enum Screen {
        case browse
        case received(Snippet)
    }

    var screen: Screen = .browse
    var path: [Track] = []
    let catalog = CatalogModel()

    @ObservationIgnored var send: (Snippet) async throws -> Void = { _ in }
    @ObservationIgnored var expand: () -> Void = {}
    @ObservationIgnored var open: (URL) -> Void = { _ in }
}
