import SwiftUI

struct ArtifactRowView: View {
    let artifact: Artifact_V1_Artifact

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: kindSystemImage(artifact.kind))
                .foregroundStyle(kindColor(artifact.kind))
                .frame(width: 16)

            VStack(alignment: .leading, spacing: 2) {
                Text(artifact.title)
                    .font(.body)
                    .lineLimit(1)

                if !artifact.content.isEmpty {
                    Text(artifact.content)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }

            Spacer()

            HeatIndicatorView(score: artifact.heatScore)
        }
        .padding(.vertical, 2)
    }

    func kindSystemImage(_ kind: Artifact_V1_ArtifactKind) -> String {
        switch kind {
        case .reminder: return "bell.circle"
        case .note: return "note.text"
        case .file: return "doc.text"
        case .email: return "envelope"
        case .bookmark: return "bookmark"
        case .log: return "terminal"
        case .task: return "checkmark.circle"
        default: return "sparkles"
        }
    }

    func kindColor(_ kind: Artifact_V1_ArtifactKind) -> Color {
        switch kind {
        case .reminder: return .red
        case .note: return .green
        case .file: return .blue
        case .email: return .purple
        case .bookmark: return .yellow
        case .log: return .orange
        case .task: return .teal
        default: return .gray
        }
    }
}

struct HeatIndicatorView: View {
    let score: Double

    var body: some View {
        Circle()
            .fill(heatColor)
            .frame(width: 8, height: 8)
    }

    var heatColor: Color {
        if score >= 0.8 { return .red }
        if score >= 0.6 { return .orange }
        if score >= 0.35 { return .yellow }
        return .green
    }
}
