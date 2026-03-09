import SwiftUI

struct SettingsView: View {
    var body: some View {
        TabView {
            GeneralSettingsView()
                .tabItem { Label("General", systemImage: "gear") }
            IntegrationsSettingsView()
                .tabItem { Label("Integrations", systemImage: "puzzlepiece.extension") }
            AISettingsView()
                .tabItem { Label("AI", systemImage: "sparkles") }
        }
        .frame(width: 450, height: 280)
    }
}

struct GeneralSettingsView: View {
    @AppStorage("dataDir") private var dataDir = "~/.local/share/clara"
    @AppStorage("logLevel") private var logLevel = "info"

    var body: some View {
        Form {
            TextField("Data directory:", text: $dataDir)
            Picker("Log level:", selection: $logLevel) {
                Text("Debug").tag("debug")
                Text("Info").tag("info")
                Text("Warn").tag("warn")
                Text("Error").tag("error")
            }
            .pickerStyle(.radioGroup)
        }
        .formStyle(.grouped)
        .padding()
    }
}

struct IntegrationsSettingsView: View {
    @AppStorage("remindersEnabled") private var remindersEnabled = true
    @AppStorage("filesystemEnabled") private var filesystemEnabled = true
    @AppStorage("taskwarriorEnabled") private var taskwarriorEnabled = false
    @AppStorage("watchDirs") private var watchDirs = ""

    var body: some View {
        Form {
            Section("Reminders") {
                Toggle("Enable Reminders integration", isOn: $remindersEnabled)
            }
            Section("Filesystem") {
                Toggle("Enable filesystem watching", isOn: $filesystemEnabled)
                TextField("Watch directories (one per line):", text: $watchDirs, axis: .vertical)
                    .lineLimit(3...6)
                    .disabled(!filesystemEnabled)
            }
            Section("Taskwarrior") {
                Toggle("Enable Taskwarrior integration", isOn: $taskwarriorEnabled)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}

struct AISettingsView: View {
    @AppStorage("ollamaURL") private var ollamaURL = "http://localhost:11434"
    @AppStorage("embedModel") private var embedModel = "nomic-embed-text"

    var body: some View {
        Form {
            TextField("Ollama URL:", text: $ollamaURL)
            TextField("Embed model:", text: $embedModel)
        }
        .formStyle(.grouped)
        .padding()
    }
}
