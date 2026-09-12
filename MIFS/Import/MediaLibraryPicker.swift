import MediaPlayer
import SwiftUI

struct MediaLibraryPicker: UIViewControllerRepresentable {
    let onPick: (MPMediaItem?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onPick: onPick) }

    func makeUIViewController(context: Context) -> MPMediaPickerController {
        let picker = MPMediaPickerController(mediaTypes: .music)
        picker.allowsPickingMultipleItems = false
        picker.showsCloudItems = true
        picker.showsItemsWithProtectedAssets = true
        picker.prompt = "Choose a song to clip"
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ picker: MPMediaPickerController, context: Context) {}

    final class Coordinator: NSObject, MPMediaPickerControllerDelegate {
        let onPick: (MPMediaItem?) -> Void

        init(onPick: @escaping (MPMediaItem?) -> Void) { self.onPick = onPick }

        nonisolated func mediaPicker(_ picker: MPMediaPickerController, didPickMediaItems collection: MPMediaItemCollection) {
            // MediaPlayer calls its picker delegate on the main thread.
            nonisolated(unsafe) let item = collection.items.first
            MainActor.assumeIsolated { onPick(item) }
        }

        nonisolated func mediaPickerDidCancel(_ picker: MPMediaPickerController) {
            MainActor.assumeIsolated { onPick(nil) }
        }
    }
}
