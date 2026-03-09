import SwiftUI

struct RelatedItemsView: View {
    let related: [Artifact_V1_Artifact]
    let onSelect: (String) -> Void

    var body: some View {
        if !related.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text("Related")
                    .font(.headline)
                    .foregroundStyle(.secondary)

                ForEach(related, id: \.id) { artifact in
                    Button(action: { onSelect(artifact.id) }) {
                        ArtifactRowView(artifact: artifact)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }
}
